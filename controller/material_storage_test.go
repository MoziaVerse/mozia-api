package controller

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 1x1 PNG，sniff 必须判成 image/png
var tinyPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
	0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE,
	0x42, 0x60, 0x82,
}

type fakeOSS struct {
	server *httptest.Server
	mu     sync.Mutex
	puts   []fakeOSSPut
}

type fakeOSSPut struct {
	path          string
	contentType   string
	authorization string
	contentSha256 string
	contentLength int64
	body          []byte
}

func newFakeOSS(t *testing.T) *fakeOSS {
	t.Helper()
	f := &fakeOSS{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.puts = append(f.puts, fakeOSSPut{
			path:          r.URL.Path,
			contentType:   r.Header.Get("Content-Type"),
			authorization: r.Header.Get("Authorization"),
			contentSha256: r.Header.Get("X-Amz-Content-Sha256"),
			contentLength: r.ContentLength,
			body:          body,
		})
		f.mu.Unlock()
		if r.Method != http.MethodPut {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeOSS) storage() *service.MaterialObjectStorage {
	fixed := time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)
	return &service.MaterialObjectStorage{
		Endpoint:         f.server.URL,
		InternalEndpoint: "http://172.16.10.151:30333",
		Bucket:           "h3-inputs",
		Region:           "us-east-1",
		AccessKeyID:      "test-ak",
		AccessKeySecret:  "test-sk",
		Prefix:           "materials/",
		Now:              func() time.Time { return fixed },
	}
}

func setMaterialTestFetchSettingAllowPrivate(t *testing.T) {
	t.Helper()
	fs := system_setting.GetFetchSetting()
	prev := *fs
	fs.EnableSSRFProtection = false
	t.Cleanup(func() { *fs = prev })
}

func setupMaterialStorageTest(t *testing.T) *fakeOSS {
	t.Helper()
	db := setupMaterialControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Material{}))
	f := newFakeOSS(t)
	service.SetMaterialObjectStorage(f.storage())
	t.Cleanup(func() { service.SetMaterialObjectStorage(nil) })
	return f
}

func multipartBody(t *testing.T, fieldName string, filename string, contentType string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	require.NoError(t, writer.WriteField("model", "seedance_2"))
	header := make(map[string][]string)
	header["Content-Disposition"] = []string{`form-data; name="` + fieldName + `"; filename="` + filename + `"`}
	if contentType != "" {
		header["Content-Type"] = []string{contentType}
	}
	part, err := writer.CreatePart(header)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return body, writer.FormDataContentType()
}

func TestUploadMaterialStoresToObjectStorage(t *testing.T) {
	f := setupMaterialStorageTest(t)
	body, contentType := multipartBody(t, "file", "reference.png", "image/png", tinyPNG)
	ctx, recorder := newMaterialContext(http.MethodPost, "/v1/sd/upload", contentType, body)
	common.SetContextKey(ctx, constant.ContextKeyUserId, 42)
	common.SetContextKey(ctx, constant.ContextKeyTokenId, 7)

	UploadMaterial(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	sum := sha256.Sum256(tinyPNG)
	wantKey := "materials/2026/09/20/" + hex.EncodeToString(sum[:])[:32] + ".png"
	fileURL, _ := resp["file_url"].(string)
	parsed, err := url.Parse(fileURL)
	require.NoError(t, err)
	assert.Equal(t, strings.TrimPrefix(f.server.URL, "http://"), parsed.Host, "客户拿到的是公网门")
	assert.Equal(t, "/h3-inputs/"+wantKey, parsed.Path)
	assert.Equal(t, "604800", parsed.Query().Get("X-Amz-Expires"))
	assert.NotEmpty(t, parsed.Query().Get("X-Amz-Signature"))
	assert.Equal(t, "image", resp["file_type"])
	assert.Equal(t, "reference.png", resp["file_name"])
	assert.Equal(t, hex.EncodeToString(sum[:]), resp["sha256"])

	require.Len(t, f.puts, 1)
	put := f.puts[0]
	assert.Equal(t, "/h3-inputs/"+wantKey, put.path)
	assert.Equal(t, "image/png", put.contentType)
	assert.True(t, strings.HasPrefix(put.authorization, "AWS4-HMAC-SHA256 Credential=test-ak/20260920/us-east-1/s3/aws4_request"), put.authorization)
	assert.Equal(t, hex.EncodeToString(sum[:]), put.contentSha256)
	assert.Equal(t, int64(len(tinyPNG)), put.contentLength)
	assert.Equal(t, tinyPNG, put.body)

	var records []model.Material
	require.NoError(t, model.DB.Find(&records).Error)
	require.Len(t, records, 1)
	assert.Equal(t, 42, records[0].UserId)
	assert.Equal(t, 7, records[0].TokenId)
	assert.Equal(t, hex.EncodeToString(sum[:]), records[0].Sha256)
	assert.Equal(t, "upload", records[0].Source)
}

func TestUploadMaterialPresignedURLIsReproducibleAndRewritable(t *testing.T) {
	f := setupMaterialStorageTest(t)
	body, contentType := multipartBody(t, "file", "a.png", "image/png", tinyPNG)
	ctx, recorder := newMaterialContext(http.MethodPost, "/v1/sd/upload", contentType, body)
	UploadMaterial(ctx)
	require.Equal(t, http.StatusOK, recorder.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	fileURL := resp["file_url"].(string)

	// 固定时钟下同一存储复算公网签名必须一致
	sum := sha256.Sum256(tinyPNG)
	key := "materials/2026/09/20/" + hex.EncodeToString(sum[:])[:32] + ".png"
	again, err := f.storage().PublicURL(key)
	require.NoError(t, err)
	assert.Equal(t, again, fileURL)

	// 派给自托管 H3 时换成内网门，key 不变，签名换掉
	internal := service.RewriteMaterialURLForCluster(fileURL)
	parsed, err := url.Parse(internal)
	require.NoError(t, err)
	assert.Equal(t, "172.16.10.151:30333", parsed.Host)
	assert.Equal(t, "/h3-inputs/"+key, parsed.Path)
	assert.Equal(t, "86400", parsed.Query().Get("X-Amz-Expires"))
	assert.NotEqual(t, fileURL, internal)

	// 不是本存储的 URL 原样返回
	assert.Equal(t, "https://example.com/x.png", service.RewriteMaterialURLForCluster("https://example.com/x.png"))
	// 同 host 但不是本桶也原样返回
	other := strings.Replace(fileURL, "/h3-inputs/", "/other-bucket/", 1)
	assert.Equal(t, other, service.RewriteMaterialURLForCluster(other))
}

func TestUploadMaterialRejectsUnsupportedType(t *testing.T) {
	f := setupMaterialStorageTest(t)
	body, contentType := multipartBody(t, "file", "notes.txt", "text/plain", []byte("hello world, this is text"))
	ctx, recorder := newMaterialContext(http.MethodPost, "/v1/sd/upload", contentType, body)
	UploadMaterial(ctx)
	assert.Equal(t, http.StatusUnsupportedMediaType, recorder.Code, recorder.Body.String())
	assert.Empty(t, f.puts)
}

func TestUploadMaterialFallsBackToDeclaredTypeWhenSniffIsOctetStream(t *testing.T) {
	f := setupMaterialStorageTest(t)
	// 随机字节 sniff 是 application/octet-stream，靠声明的 Content-Type 兜底
	blob := bytes.Repeat([]byte{0x00, 0xFF, 0x13, 0x37}, 300)
	body, contentType := multipartBody(t, "file", "clip.mov", "video/quicktime", blob)
	ctx, recorder := newMaterialContext(http.MethodPost, "/v1/sd/upload", contentType, body)
	UploadMaterial(ctx)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.Len(t, f.puts, 1)
	assert.Equal(t, "video/quicktime", f.puts[0].contentType)
	assert.True(t, strings.HasSuffix(f.puts[0].path, ".mov"))
}

func TestUploadMaterialRejectsOversizedImage(t *testing.T) {
	f := setupMaterialStorageTest(t)
	oversized := append(append([]byte{}, tinyPNG...), bytes.Repeat([]byte{0}, materialMaxImageBytes)...)
	body, contentType := multipartBody(t, "file", "big.png", "image/png", oversized)
	ctx, recorder := newMaterialContext(http.MethodPost, "/v1/sd/upload", contentType, body)
	UploadMaterial(ctx)
	assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code, recorder.Body.String())
	assert.Empty(t, f.puts)
}

func TestUploadMaterialRequiresFileField(t *testing.T) {
	f := setupMaterialStorageTest(t)
	body, contentType := multipartBody(t, "attachment", "a.png", "image/png", tinyPNG)
	ctx, recorder := newMaterialContext(http.MethodPost, "/v1/sd/upload", contentType, body)
	UploadMaterial(ctx)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Empty(t, f.puts)
}

func TestImportMaterialFetchesSourceAndStores(t *testing.T) {
	f := setupMaterialStorageTest(t)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/pic/reference.png", r.URL.Path)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(tinyPNG)
	}))
	defer source.Close()
	setMaterialTestFetchSettingAllowPrivate(t)

	body := strings.NewReader(`{"url":"` + source.URL + `/pic/reference.png"}`)
	ctx, recorder := newMaterialContext(http.MethodPost, "/v1/sd/upload_url", "application/json", body)
	ImportMaterial(ctx)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.Equal(t, "image", resp["file_type"])
	assert.Equal(t, "reference.png", resp["file_name"])
	require.Len(t, f.puts, 1)
	assert.Equal(t, tinyPNG, f.puts[0].body)

	var records []model.Material
	require.NoError(t, model.DB.Find(&records).Error)
	require.Len(t, records, 1)
	assert.Equal(t, "import", records[0].Source)
}

func TestImportMaterialRejectsRedirects(t *testing.T) {
	f := setupMaterialStorageTest(t)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:5432/internal", http.StatusFound)
	}))
	defer source.Close()
	setMaterialTestFetchSettingAllowPrivate(t)

	body := strings.NewReader(`{"url":"` + source.URL + `/redirect"}`)
	ctx, recorder := newMaterialContext(http.MethodPost, "/v1/sd/upload_url", "application/json", body)
	ImportMaterial(ctx)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "redirect")
	assert.Empty(t, f.puts)
}

func TestImportMaterialRejectsNon200Source(t *testing.T) {
	f := setupMaterialStorageTest(t)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer source.Close()
	setMaterialTestFetchSettingAllowPrivate(t)

	body := strings.NewReader(`{"url":"` + source.URL + `/missing.png"}`)
	ctx, recorder := newMaterialContext(http.MethodPost, "/v1/sd/upload_url", "application/json", body)
	ImportMaterial(ctx)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "404")
	assert.Empty(t, f.puts)
}

func TestUploadMaterialStillProxiesToCoolWithoutStorageConfig(t *testing.T) {
	// 没配 OSS 时必须走原路径（回滚 = 删 env）
	service.SetMaterialObjectStorage(nil)
	db := setupMaterialControllerTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/cool/upload", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"file_url":"https://cdn.cool.example/x.png"}`))
	}))
	defer upstream.Close()
	createMaterialChannel(t, db, 9, upstream.URL, "default", 1)

	body, contentType := multipartBody(t, "file", "a.png", "image/png", tinyPNG)
	ctx, recorder := newMaterialContext(http.MethodPost, "/v1/sd/upload", contentType, body)
	UploadMaterial(ctx)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "cdn.cool.example")
}

func TestDetectMaterialMime(t *testing.T) {
	cases := []struct {
		name     string
		head     []byte
		declared string
		filename string
		want     string
		ok       bool
	}{
		{"png by sniff", tinyPNG, "", "", "image/png", true},
		{"sniff wins over wrong declared", tinyPNG, "video/mp4", "a.mp4", "image/png", true},
		{"octet-stream falls back to declared", []byte{0, 1, 2, 3}, "audio/mpeg", "", "audio/mpeg", true},
		{"octet-stream falls back to extension", []byte{0, 1, 2, 3}, "", "voice.m4a", "audio/mp4", true},
		{"octet-stream with nothing else", []byte{0, 1, 2, 3}, "", "blob.bin", "", false},
		{"text is rejected even if declared image", []byte("plain text content here"), "image/png", "a.png", "", false},
		{"declared type outside whitelist", []byte{0, 1, 2, 3}, "application/pdf", "a.pdf", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := detectMaterialMime(tc.head, tc.declared, tc.filename)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestImportMaterialRejectsPrivateResolutionAtDial(t *testing.T) {
	// SSRF 防护开启时，即便 URL 层校验被绕过，拨号层也必须拒绝解析到内网的主机
	f := setupMaterialStorageTest(t)
	fs := system_setting.GetFetchSetting()
	prev := *fs
	fs.EnableSSRFProtection = true
	fs.AllowPrivateIp = false
	t.Cleanup(func() { *fs = prev })

	_, err := materialImportClient(t.Context(), "localhost")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "private address")
	assert.Empty(t, f.puts)
}
