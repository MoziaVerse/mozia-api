package seedance

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// prepareMultipartRequest builds a multipart submit carrying scalar fields plus
// binary reference images, mirroring what a caller uploads when it cannot
// publish assets to a public URL.
func prepareMultipartRequest(
	t *testing.T,
	fields map[string]string,
	files map[string][]byte,
) (*TaskAdaptor, *gin.Context, *relaycommon.RelayInfo) {
	t.Helper()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	for key, value := range fields {
		require.NoError(t, writer.WriteField(key, value))
	}
	for name, content := range files {
		part, err := writer.CreateFormFile(name, name+".png")
		require.NoError(t, err)
		_, err = part.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewReader(buf.Bytes()))
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())

	info := &relaycommon.RelayInfo{
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		OriginModelName: "minimax/minimax-h3-ref2va",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    "https://provider.example",
			UpstreamModelName: "minimax/minimax-h3-ref2va",
		},
	}
	// In production the distributor middleware has already run
	// UnmarshalBodyReusable, which caches the body into BodyStorage and swaps
	// c.Request.Body for a seekable handle. Without that, c.MultipartForm()
	// consumes the one-shot reader and the later re-read fails with
	// "multipart: NextPart: EOF". Reproduce that setup here.
	var discard map[string]any
	require.NoError(t, common.UnmarshalBodyReusable(c, &discard))

	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	return adaptor, c, info
}

// readBackParts re-parses the outbound body so assertions describe what the
// upstream actually receives.
func readBackParts(t *testing.T, c *gin.Context, body io.Reader) (map[string]string, map[string][]byte) {
	t.Helper()

	boundary, ok := c.Get(multipartBoundaryKey)
	require.True(t, ok, "BuildRequestBody must publish the boundary for BuildRequestHeader")
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	reader := multipart.NewReader(bytes.NewReader(data), boundary.(string))
	form, err := reader.ReadForm(32 << 20)
	require.NoError(t, err)
	t.Cleanup(func() { _ = form.RemoveAll() })

	values := make(map[string]string, len(form.Value))
	for key, vals := range form.Value {
		require.NotEmpty(t, vals)
		values[key] = vals[0]
	}
	files := make(map[string][]byte, len(form.File))
	for key, headers := range form.File {
		require.NotEmpty(t, headers)
		f, err := headers[0].Open()
		require.NoError(t, err)
		content, err := io.ReadAll(f)
		require.NoError(t, err)
		_ = f.Close()
		files[key] = content
	}
	return values, files
}

// The whole point of the change: binary reference images must survive the hop.
// Before this, the gateway decoded the body into a struct and re-marshalled it
// as JSON, silently dropping every file part.
func TestBuildRequestBodyForwardsUploadedReferenceImages(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nfake-bytes")
	adaptor, c, info := prepareMultipartRequest(t,
		map[string]string{
			"model":    "minimax/minimax-h3-ref2va",
			"prompt":   "a cat in the style of Picture 1",
			"duration": "5",
		},
		map[string][]byte{"reference_images": png},
	)

	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	values, files := readBackParts(t, c, body)

	require.Contains(t, files, "reference_images", "uploaded file must be forwarded, not dropped")
	assert.Equal(t, png, files["reference_images"])
	assert.Equal(t, "minimax/minimax-h3-ref2va", values["model"])
	assert.Equal(t, "5", values["duration"])
	assert.Equal(t, "true", values["async"])
}

// Normalized scalars drive routing and billing, so a client must not be able to
// override them by sending its own copy — the same guarantee the JSON path has.
func TestMultipartCannotOverrideNormalizedFields(t *testing.T) {
	adaptor, c, info := prepareMultipartRequest(t,
		map[string]string{
			"model":    "minimax/minimax-h3-ref2va",
			"prompt":   "real prompt",
			"duration": "5",
			"async":    "false",
		},
		map[string][]byte{"reference_images": []byte("\x89PNG\r\n\x1a\nx")},
	)

	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	values, _ := readBackParts(t, c, body)

	assert.Equal(t, "true", values["async"], "async is gateway-controlled")
	assert.Equal(t, "5", values["duration"])
	assert.Equal(t, "real prompt", values["prompt"])
}

// Requests without files must keep emitting JSON byte-for-byte as before, so
// every existing provider on this adaptor is unaffected.
func TestJSONRequestsRemainJSON(t *testing.T) {
	adaptor, c, info := prepareRequest(t, `{
		"model":"cool:seedance_2_720p",
		"prompt":"hello",
		"duration":5
	}`)

	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)

	_, hasBoundary := c.Get(multipartBoundaryKey)
	assert.False(t, hasBoundary, "JSON path must not switch to multipart")

	data, err := io.ReadAll(body)
	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(bytes.TrimSpace(data), []byte("{")), "expected a JSON object")
}

// The Content-Type must advertise the exact boundary used to encode the body,
// otherwise the upstream cannot split the parts.
func TestBuildRequestHeaderAdvertisesMultipartBoundary(t *testing.T) {
	adaptor, c, info := prepareMultipartRequest(t,
		map[string]string{
			"model":    "minimax/minimax-h3-ref2va",
			"prompt":   "p",
			"duration": "5",
		},
		map[string][]byte{"reference_images": []byte("\x89PNG\r\n\x1a\nx")},
	)
	info.ApiKey = "sk-test"

	_, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)

	upstream := httptest.NewRequest(http.MethodPost, "https://provider.example/v1/videos", nil)
	require.NoError(t, adaptor.BuildRequestHeader(c, upstream, info))

	mediaType, params, err := mime.ParseMediaType(upstream.Header.Get("Content-Type"))
	require.NoError(t, err)
	assert.Equal(t, "multipart/form-data", mediaType)

	boundary, _ := c.Get(multipartBoundaryKey)
	assert.Equal(t, boundary.(string), params["boundary"])
}

// A JSON submit must still declare application/json.
func TestBuildRequestHeaderKeepsJSONContentType(t *testing.T) {
	adaptor, c, info := prepareRequest(t, `{
		"model":"cool:seedance_2_720p",
		"prompt":"hello",
		"duration":5
	}`)
	info.ApiKey = "sk-test"

	_, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)

	upstream := httptest.NewRequest(http.MethodPost, "https://provider.example/v1/videos", nil)
	require.NoError(t, adaptor.BuildRequestHeader(c, upstream, info))
	assert.Equal(t, "application/json", upstream.Header.Get("Content-Type"))
}

// Reference clips and audio must ride along too: the upstream classifies file
// parts by field name and accepts audio/video directly, so the gateway has no
// reason to treat them differently from images.
func TestBuildRequestBodyForwardsReferenceVideoAndAudio(t *testing.T) {
	adaptor, c, info := prepareMultipartRequest(t,
		map[string]string{
			"model":    "minimax/minimax-h3-ref2va",
			"prompt":   "a cat, mood of Audio 1",
			"duration": "5",
		},
		map[string][]byte{
			"reference_images": []byte("\x89PNG\r\n\x1a\nimg"),
			"reference_videos": []byte("\x00\x00\x00 ftypmp42clip"),
			"reference_audios": []byte("ID3audio"),
		},
	)

	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	_, files := readBackParts(t, c, body)

	assert.Equal(t, []byte("\x89PNG\r\n\x1a\nimg"), files["reference_images"])
	assert.Equal(t, []byte("\x00\x00\x00 ftypmp42clip"), files["reference_videos"])
	assert.Equal(t, []byte("ID3audio"), files["reference_audios"])
}

// Uploaded clips and audio must not inflate the reference-image count: the
// per-model limit is about images only. Nine images plus a clip is legal;
// counting the clip would reject it as "at most 9 reference images".
func TestUploadedClipsDoNotCountAsReferenceImages(t *testing.T) {
	files := map[string][]byte{"reference_videos": []byte("\x00\x00\x00 ftypmp42")}
	for i := 0; i < 9; i++ {
		files["image"+strconv.Itoa(i)] = []byte("\x89PNG\r\n\x1a\n")
	}

	adaptor, c, info := prepareMultipartRequest(t,
		map[string]string{
			"model":    "minimax/minimax-h3-ref2va",
			"prompt":   "p",
			"duration": "5",
		},
		files,
	)

	// Reaching BuildRequestBody at all proves validation accepted 9 images
	// alongside the clip.
	_, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	assert.Equal(t, 9, uploadedImageCount(c))
}
