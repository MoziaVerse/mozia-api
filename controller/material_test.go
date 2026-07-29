package controller

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMaterialControllerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)

	originalDB := model.DB
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
	model.DB = db

	t.Cleanup(func() {
		model.DB = originalDB
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func newMaterialContext(method string, target string, contentType string, body io.Reader) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, body)
	ctx.Request.Header.Set("Content-Type", contentType)
	common.SetContextKey(ctx, constant.ContextKeyUsingGroup, "default")
	common.SetContextKey(ctx, constant.ContextKeyUserGroup, "default")
	return ctx, recorder
}

func createMaterialChannel(t *testing.T, db *gorm.DB, id int, baseURL string, group string, priority int64) {
	t.Helper()
	require.NoError(t, db.Create(&model.Channel{
		Id:       id,
		Name:     fmt.Sprintf("material-%d", id),
		Type:     constant.ChannelTypeMoziaCool,
		Key:      fmt.Sprintf("upstream-key-%d", id),
		Status:   common.ChannelStatusEnabled,
		BaseURL:  &baseURL,
		Group:    group,
		Priority: &priority,
	}).Error)
}

func TestUploadMaterialForwardsMultipartToInternalProviderEndpoint(t *testing.T) {
	db := setupMaterialControllerTestDB(t)

	upstreamCalled := false
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalled = true
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/v1/cool/upload", r.URL.Path)
		assert.Equal(t, "Bearer upstream-key-4", r.Header.Get("Authorization"))
		require.NoError(t, r.ParseMultipartForm(1<<20))
		assert.Equal(t, "seedance_2", r.FormValue("model"))
		file, header, err := r.FormFile("file")
		require.NoError(t, err)
		defer file.Close()
		assert.Equal(t, "reference.png", header.Filename)
		data, err := io.ReadAll(file)
		require.NoError(t, err)
		assert.Equal(t, []byte("image-bytes"), data)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"file_url":"https://files.example/reference.png"}`))
	}))
	defer upstream.Close()

	// A higher-priority channel outside the caller's group must not be selected.
	createMaterialChannel(t, db, 1, "http://127.0.0.1:1", "vip", 100)
	createMaterialChannel(t, db, 2, "http://127.0.0.1:1", "default", 1)
	createMaterialChannel(t, db, 4, upstream.URL, "team,default", 10)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "seedance_2"))
	part, err := writer.CreateFormFile("file", "reference.png")
	require.NoError(t, err)
	_, err = part.Write([]byte("image-bytes"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	ctx, recorder := newMaterialContext(http.MethodPost, "/v1/sd/upload", writer.FormDataContentType(), &body)
	UploadMaterial(ctx)

	require.True(t, upstreamCalled)
	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.JSONEq(t, `{"file_url":"https://files.example/reference.png"}`, recorder.Body.String())
}

func TestImportMaterialForwardsJSONToInternalProviderEndpoint(t *testing.T) {
	db := setupMaterialControllerTestDB(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/cool/upload_url", r.URL.Path)
		assert.Equal(t, "Bearer upstream-key-3", r.Header.Get("Authorization"))
		assert.Empty(t, r.Header.Get("Cookie"))
		assert.Empty(t, r.Header.Get("X-Api-Key"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.JSONEq(t, `{"url":"https://1.1.1.1/reference.mp4","filename":"reference.mp4"}`, string(body))
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream-Internal", "must-not-leak")
		_, _ = w.Write([]byte(`{"file_url":"https://files.example/reference.mp4"}`))
	}))
	defer upstream.Close()
	createMaterialChannel(t, db, 3, upstream.URL, "default", 10)

	requestBody := `{"url":"https://1.1.1.1/reference.mp4","filename":"reference.mp4"}`
	ctx, recorder := newMaterialContext(http.MethodPost, "/v1/sd/upload_url", "application/json", strings.NewReader(requestBody))
	ctx.Request.Header.Set("Authorization", "Bearer client-token")
	ctx.Request.Header.Set("Cookie", "session=client-secret")
	ctx.Request.Header.Set("X-Api-Key", "client-api-key")
	ImportMaterial(ctx)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	assert.Empty(t, recorder.Header().Get("X-Upstream-Internal"))
	assert.JSONEq(t, `{"file_url":"https://files.example/reference.mp4"}`, recorder.Body.String())
}

func TestImportMaterialRejectsInvalidURLsBeforeChannelSelection(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid JSON", body: `{"url":`},
		{name: "missing URL", body: `{}`},
		{name: "unsupported scheme", body: `{"url":"file:///etc/passwd"}`},
		{name: "relative URL", body: `{"url":"/reference.png"}`},
		{name: "embedded credentials", body: `{"url":"https://user:password@1.1.1.1/reference.png"}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, recorder := newMaterialContext(http.MethodPost, "/v1/sd/upload_url", "application/json", strings.NewReader(test.body))
			ImportMaterial(ctx)

			assert.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "invalid_request_error")
			assert.NotContains(t, strings.ToLower(recorder.Body.String()), "cool")
		})
	}
}

func TestImportMaterialRejectsOversizedRequestBody(t *testing.T) {
	ctx, recorder := newMaterialContext(
		http.MethodPost,
		"/v1/sd/upload_url",
		"application/json",
		strings.NewReader(`{"url":"https://1.1.1.1/reference.png"}`),
	)
	ctx.Request.Body = http.MaxBytesReader(recorder, ctx.Request.Body, 8)

	ImportMaterial(ctx)

	assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "request_too_large")
}

func TestMaterialEndpointsRejectUnexpectedContentTypes(t *testing.T) {
	tests := []struct {
		name        string
		target      string
		contentType string
		handler     gin.HandlerFunc
	}{
		{name: "local upload", target: "/v1/sd/upload", contentType: "application/json", handler: UploadMaterial},
		{name: "URL import", target: "/v1/sd/upload_url", contentType: "text/plain", handler: ImportMaterial},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, recorder := newMaterialContext(http.MethodPost, test.target, test.contentType, strings.NewReader("{}"))
			test.handler(ctx)

			assert.Equal(t, http.StatusUnsupportedMediaType, recorder.Code)
			assert.Contains(t, recorder.Body.String(), "unsupported_media_type")
		})
	}
}

func TestImportMaterialReturnsServiceUnavailableWithoutMaterialChannel(t *testing.T) {
	setupMaterialControllerTestDB(t)
	ctx, recorder := newMaterialContext(http.MethodPost, "/v1/sd/upload_url", "application/json", strings.NewReader(`{"url":"https://1.1.1.1/a.png"}`))

	ImportMaterial(ctx)

	assert.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "material_channel_unavailable")
	assert.NotContains(t, strings.ToLower(recorder.Body.String()), "cool")
}
