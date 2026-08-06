package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSignedVideoProxyStreamsAttachment(t *testing.T) {
	prepareVideoProxyTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/videos/upstream-task/content", r.URL.Path)
		assert.Equal(t, "Bearer provider-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Set-Cookie", "upstream=blocked")
		_, _ = w.Write([]byte("video-bytes"))
	}))
	defer upstream.Close()

	baseURL := upstream.URL
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:      1,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "provider-key",
		BaseURL: &baseURL,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Task{
		TaskID:    "task_public",
		UserId:    42,
		ChannelId: 1,
		Status:    model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "upstream-task",
		},
		Data: []byte(`{"output":{"filename":"clip.mp4"}}`),
	}).Error)

	r := buildVideoProxyTestRouter(t)
	now := time.Now()
	signedURL := taskcommon.BuildSignedVideoProxyURLAt(now, 42, "task_public", "clip.mp4")
	req := httptest.NewRequest(http.MethodGet, mustRequestURI(t, signedURL), nil)
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "video-bytes", recorder.Body.String())
	assert.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
	assert.Empty(t, recorder.Header().Get("Set-Cookie"))
	assert.Contains(t, recorder.Header().Get("Content-Disposition"), "attachment")
	assert.Contains(t, recorder.Header().Get("Content-Disposition"), "clip.mp4")
	assert.Equal(t, "private, max-age=900", recorder.Header().Get("Cache-Control"))

	require.NoError(t, model.DB.Create(&model.Channel{
		Id:   2,
		Type: constant.ChannelTypeMiniMax,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Task{
		TaskID:    "task_data",
		UserId:    42,
		ChannelId: 2,
		Status:    model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			ResultURL: "data:video/mp4;base64,dmlkZW8tYnl0ZXM=",
		},
	}).Error)

	dataURL := taskcommon.BuildSignedVideoProxyURLAt(now, 42, "task_data", "inline.mp4")
	dataRequest := httptest.NewRequest(http.MethodGet, mustRequestURI(t, dataURL), nil)
	dataRecorder := httptest.NewRecorder()
	r.ServeHTTP(dataRecorder, dataRequest)

	assert.Equal(t, http.StatusOK, dataRecorder.Code)
	assert.Equal(t, "video-bytes", dataRecorder.Body.String())
	assert.Contains(t, dataRecorder.Header().Get("Content-Disposition"), "inline.mp4")
	assert.Equal(t, "private, max-age=900", dataRecorder.Header().Get("Cache-Control"))
}

func TestSignedVideoProxyRejectsExpiredAndTamperedURLs(t *testing.T) {
	r := buildVideoProxyTestRouter(t)
	now := time.Now()
	signedURL := taskcommon.BuildSignedVideoProxyURLAt(now, 42, "task_public", "clip.mp4")
	parsed, err := url.Parse(signedURL)
	require.NoError(t, err)

	expired := *parsed
	expired.RawQuery = replaceQueryValue(parsed.RawQuery, "expires", fmt.Sprintf("%d", now.Add(-time.Minute).Unix()))
	assertSignedVideoProxyError(t, r, expired.RequestURI(), http.StatusForbidden, "Signature expired")

	assertSignedVideoProxyError(t, r, strings.Replace(parsed.RequestURI(), "/task_public/", "/task_other/", 1), http.StatusForbidden, "Invalid signature")
	assertSignedVideoProxyError(t, r, strings.Replace(parsed.RequestURI(), "uid=42", "uid=43", 1), http.StatusForbidden, "Invalid signature")
	assertSignedVideoProxyError(t, r, strings.Replace(parsed.RequestURI(), "/clip.mp4?", "/other.mp4?", 1), http.StatusForbidden, "Invalid signature")
	assertSignedVideoProxyError(t, r, strings.Replace(parsed.RequestURI(), "expires="+parsed.Query().Get("expires"), fmt.Sprintf("expires=%d", now.Unix()+1), 1), http.StatusForbidden, "Invalid signature")
}

func buildVideoProxyTestRouter(t *testing.T) http.Handler {
	t.Helper()
	originalFetchSetting := *system_setting.GetFetchSetting()
	system_setting.GetFetchSetting().EnableSSRFProtection = false
	system_setting.GetFetchSetting().AllowPrivateIp = true
	t.Cleanup(func() {
		*system_setting.GetFetchSetting() = originalFetchSetting
	})
	service.InitHttpClient()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/videos/:task_id/content/:filename", SignedVideoProxy)
	return r
}

func prepareVideoProxyTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMemoryCacheEnabled := common.MemoryCacheEnabled
	model.DB = db
	model.LOG_DB = db
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.MemoryCacheEnabled = originalMemoryCacheEnabled
	})
}

func assertSignedVideoProxyError(t *testing.T, handler http.Handler, requestURI string, wantStatus int, wantMessage string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, requestURI, nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	assert.Equal(t, wantStatus, recorder.Code)
	assert.Contains(t, recorder.Body.String(), wantMessage)
}

func replaceQueryValue(rawQuery, key, value string) string {
	query, _ := url.ParseQuery(rawQuery)
	query.Set(key, value)
	return query.Encode()
}

func mustRequestURI(t *testing.T, signedURL string) string {
	t.Helper()
	parsed, err := url.Parse(signedURL)
	require.NoError(t, err)
	return parsed.RequestURI()
}
