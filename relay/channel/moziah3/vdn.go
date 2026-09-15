package moziah3

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const (
	vdnDuration        = 345.0 / 24
	vdnMaxImageBytes   = 10 << 20
	vdnMaxRequestBytes = 21 << 20
)

// VDN selects its generation mode from uploaded frames, independently of model names.
// Task IDs, polling and authenticated downloads share the H3 lifecycle.
type VDNTaskAdaptor struct {
	TaskAdaptor
	prompt      string
	seed        *int64
	frames      map[string][]byte
	contentType string
}

func (a *VDNTaskAdaptor) GetChannelName() string { return "moziah3-vdn" }
func (a *VDNTaskAdaptor) GetModelList() []string { return nil }

func (a *VDNTaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if strings.TrimSpace(info.ChannelBaseUrl) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("channel base URL is required"), "invalid_channel_base_url", http.StatusBadRequest)
	}
	if err := a.readRequest(c); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	info.Action = "generate"
	c.Set("task_request", relaycommon.TaskSubmitReq{Prompt: a.prompt, Model: info.OriginModelName})
	return nil
}

func (a *VDNTaskAdaptor) readRequest(c *gin.Context) error {
	isMultipart := strings.HasPrefix(c.GetHeader("Content-Type"), "multipart/form-data")
	if isMultipart {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return err
		}
		if storage.Size() > vdnMaxRequestBytes {
			return fmt.Errorf("MoziaH3-VDN multipart request exceeds 21 MiB")
		}
	}
	var fields map[string]json.RawMessage
	if err := common.UnmarshalBodyReusable(c, &fields); err != nil {
		return err
	}
	// Only these output parameters can be stated explicitly, and cannot change
	// the recipe. Reject unsupported generation controls rather than ignore them.
	for key, value := range fields {
		var expected string
		switch key {
		case "model", "prompt", "seed", "content", "first_frame", "last_frame", "input_reference", "image", "images":
			continue
		case "duration", "seconds":
			expected = "14.375"
		case "size":
			expected = "1344x768"
		case "resolution":
			expected = "768p"
		default:
			return fmt.Errorf("MoziaH3-VDN does not support parameter %q", key)
		}
		actual := string(value)
		_ = common.Unmarshal(value, &actual) // Multipart values and numeric strings.
		matches := actual == expected
		if number, err := strconv.ParseFloat(expected, 64); err == nil {
			actualNumber, err := strconv.ParseFloat(actual, 64)
			matches = err == nil && actualNumber == number
		}
		if !matches {
			return fmt.Errorf("MoziaH3-VDN fixes %s at %s; omit this parameter or use the fixed value", key, expected)
		}
	}

	data, err := common.Marshal(fields)
	if err != nil {
		return err
	}
	var req relaycommon.TaskSubmitReq
	if err := common.Unmarshal(data, &req); err != nil {
		return err
	}
	summary, err := req.ParseVideoContentWithLastFrameOnly()
	if err != nil {
		return err
	}
	if len(summary.ReferenceImages)+len(summary.ReferenceVideos)+len(summary.ReferenceAudios) > 0 {
		return fmt.Errorf("MoziaH3-VDN accepts first_frame and last_frame, not reference materials")
	}
	a.prompt = summary.Prompt
	if length := utf8.RuneCountInString(a.prompt); length == 0 || length > 16384 {
		return fmt.Errorf("prompt must contain 1–16384 characters")
	}
	a.seed = nil
	if seed, ok := fields["seed"]; ok && string(seed) != "null" {
		value := string(seed)
		_ = common.Unmarshal(seed, &value)
		number, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("seed must be an integer")
		}
		a.seed = &number
	}

	sources := map[string]string{"first_frame": summary.FirstFrameURL, "last_frame": summary.LastFrameURL}
	for _, key := range []string{"first_frame", "last_frame", "input_reference", "image"} {
		if value, ok := fields[key]; ok {
			var source string
			if err := common.Unmarshal(value, &source); err != nil || strings.TrimSpace(source) == "" {
				return fmt.Errorf("%s must be an image URL or data URL", key)
			}
			frame := key
			if key == "input_reference" || key == "image" {
				frame = "first_frame"
			}
			if sources[frame] != "" {
				return fmt.Errorf("duplicate %s", frame)
			}
			sources[frame] = source
		}
	}
	if len(req.Images) > 2 {
		return fmt.Errorf("MoziaH3-VDN accepts at most two images (first and last frame)")
	}
	for i, source := range req.Images {
		frame := []string{"first_frame", "last_frame"}[i]
		if sources[frame] != "" || strings.TrimSpace(source) == "" {
			return fmt.Errorf("duplicate or empty %s", frame)
		}
		sources[frame] = source
	}

	a.frames = make(map[string][]byte)
	if isMultipart {
		form, err := common.ParseMultipartFormReusable(c)
		if err != nil {
			return err
		}
		defer form.RemoveAll()
		for key, values := range form.Value {
			if len(values) != 1 {
				return fmt.Errorf("duplicate %s", key)
			}
		}
		for frame, files := range form.File {
			if frame != "first_frame" && frame != "last_frame" {
				return fmt.Errorf("unsupported image upload field %q", frame)
			}
			if len(files) != 1 || sources[frame] != "" {
				return fmt.Errorf("duplicate %s", frame)
			}
			if files[0].Size > vdnMaxImageBytes {
				return fmt.Errorf("%s exceeds 10 MiB", frame)
			}
			file, err := files[0].Open()
			if err != nil {
				return err
			}
			data, err := io.ReadAll(io.LimitReader(file, vdnMaxImageBytes+1))
			_ = file.Close()
			if err != nil {
				return err
			}
			a.frames[frame] = data
		}
	}
	for frame, source := range sources {
		if source == "" {
			continue
		}
		data, err := loadVDNFrame(source)
		if err != nil {
			return fmt.Errorf("%s: %w", frame, err)
		}
		a.frames[frame] = data
	}
	for frame, data := range a.frames {
		if len(data) > vdnMaxImageBytes {
			return fmt.Errorf("%s exceeds 10 MiB", frame)
		}
		if !strings.HasPrefix(http.DetectContentType(data), "image/") {
			return fmt.Errorf("%s must be an image (PNG or JPEG recommended)", frame)
		}
	}
	return nil
}

// loadVDNFrame uses the shared download policy, but bounds reads to VDN's file limit.
func loadVDNFrame(source string) ([]byte, error) {
	source = strings.TrimSpace(source)
	if strings.HasPrefix(source, "data:image/") {
		header, encoded, ok := strings.Cut(source, ",")
		if !ok || !strings.HasSuffix(header, ";base64") {
			return nil, fmt.Errorf("image data URL must use base64")
		}
		return io.ReadAll(io.LimitReader(base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded)), vdnMaxImageBytes+1))
	}
	if !strings.HasPrefix(source, "http://") && !strings.HasPrefix(source, "https://") {
		return nil, fmt.Errorf("image must use an HTTP(S) URL or image data URL")
	}
	resp, err := service.DoDownloadRequest(source, "MoziaH3-VDN frame")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image download returned HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, vdnMaxImageBytes+1))
}

func (a *VDNTaskAdaptor) EstimateBilling(_ *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	return map[string]float64{"duration": vdnDuration}
}

// BillingRequestBody describes the actual fixed output for explicit task pricing,
// including multipart clients that have no JSON body or duration field.
func (a *VDNTaskAdaptor) BillingRequestBody() ([]byte, error) {
	images := make([]string, 0, len(a.frames))
	for _, frame := range []string{"first_frame", "last_frame"} {
		if len(a.frames[frame]) > 0 {
			images = append(images, frame)
		}
	}
	return common.Marshal(map[string]any{
		"duration": vdnDuration, "seconds": vdnDuration,
		"size": "1344x768", "resolution": "768p", "fps": 24,
		"num_frames": 345, "num_inference_steps": 8, "images": images,
	})
}

func (a *VDNTaskAdaptor) BuildRequestBody(_ *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	// Model mapping is applied by the relay after validation.
	for key, value := range map[string]string{"model": info.UpstreamModelName, "prompt": a.prompt} {
		if err := writer.WriteField(key, value); err != nil {
			return nil, err
		}
	}
	if a.seed != nil {
		if err := writer.WriteField("seed", strconv.FormatInt(*a.seed, 10)); err != nil {
			return nil, err
		}
	}
	for _, frame := range []string{"first_frame", "last_frame"} {
		data, ok := a.frames[frame]
		if !ok {
			continue
		}
		mimeType := http.DetectContentType(data)
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s.%s"`, frame, frame, strings.TrimPrefix(mimeType, "image/")))
		header.Set("Content-Type", mimeType)
		part, err := writer.CreatePart(header)
		if err != nil {
			return nil, err
		}
		if _, err := part.Write(data); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if body.Len() > vdnMaxRequestBytes {
		return nil, fmt.Errorf("MoziaH3-VDN multipart request exceeds 21 MiB")
	}
	a.contentType = writer.FormDataContentType()
	return bytes.NewReader(body.Bytes()), nil
}

func (a *VDNTaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+info.ApiKey)
	req.Header.Set("Content-Type", a.contentType)
	return nil
}

func (a *VDNTaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (*http.Response, error) {
	resp, err := channel.DoTaskApiRequest(a, c, info, body)
	if err == nil && resp != nil && resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		resp.StatusCode = http.StatusOK
	}
	return resp, err
}
