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
	assert.Equal(t, "private, max-age=86400", recorder.Header().Get("Cache-Control"))

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
	assert.Equal(t, "private, max-age=86400", dataRecorder.Header().Get("Cache-Control"))
}

func TestSignedVideoProxyForwardsRangeAndSupportsHead(t *testing.T) {
	prepareVideoProxyTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Type", "video/mp4")
		switch r.Method {
		case http.MethodGet:
			assert.Equal(t, "bytes=0-4", r.Header.Get("Range"))
			w.Header().Set("Content-Range", "bytes 0-4/11")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("video"))
		case http.MethodHead:
			assert.Empty(t, r.Header.Get("Range"))
			w.Header().Set("Content-Length", "11")
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer upstream.Close()

	baseURL := upstream.URL
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:      3,
		Type:    constant.ChannelTypeOpenAI,
		Key:     "provider-key",
		BaseURL: &baseURL,
	}).Error)
	require.NoError(t, model.DB.Create(&model.Task{
		TaskID:    "task_range",
		UserId:    42,
		ChannelId: 3,
		Status:    model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "upstream-task",
		},
	}).Error)

	r := buildVideoProxyTestRouter(t)
	signedURL := taskcommon.BuildSignedVideoGenerationURLAt(time.Now(), 42, "task_range", "clip.mp4")

	getRequest := httptest.NewRequest(http.MethodGet, mustRequestURI(t, signedURL), nil)
	getRequest.Header.Set("Range", "bytes=0-4")
	getRecorder := httptest.NewRecorder()
	r.ServeHTTP(getRecorder, getRequest)
	assert.Equal(t, http.StatusPartialContent, getRecorder.Code)
	assert.Equal(t, "video", getRecorder.Body.String())
	assert.Equal(t, "bytes 0-4/11", getRecorder.Header().Get("Content-Range"))

	headRequest := httptest.NewRequest(http.MethodHead, mustRequestURI(t, signedURL), nil)
	headRecorder := httptest.NewRecorder()
	r.ServeHTTP(headRecorder, headRequest)
	assert.Equal(t, http.StatusOK, headRecorder.Code)
	assert.Empty(t, headRecorder.Body.String())
	assert.Equal(t, "11", headRecorder.Header().Get("Content-Length"))
}

func TestSignedVideoProxyAllowsConfiguredChannelOriginOnly(t *testing.T) {
	prepareVideoProxyTestDB(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/videos/upstream-port-task/content", r.URL.Path)
		assert.Equal(t, "Bearer provider-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte("video-bytes"))
	}))
	defer upstream.Close()

	baseURL := upstream.URL
	require.NoError(t, model.DB.Create(&model.Channel{
		Id:      1001,
		Type:    constant.ChannelTypeMoziaSeedanceVideos,
		Key:     "provider-key",
		BaseURL: &baseURL,
	}).Error)
	for _, task := range []*model.Task{
		{
			TaskID:    "task_channel_port",
			UserId:    84,
			ChannelId: 1001,
			Status:    model.TaskStatusSuccess,
			PrivateData: model.TaskPrivateData{
				UpstreamTaskID: "upstream-port-task",
			},
		},
		{
			TaskID:    "task_result_port",
			UserId:    84,
			ChannelId: 1001,
			Status:    model.TaskStatusSuccess,
			PrivateData: model.TaskPrivateData{
				ResultURL: upstream.URL + "/untrusted.mp4",
			},
		},
	} {
		require.NoError(t, model.DB.Create(task).Error)
	}

	fetchSetting := system_setting.GetFetchSetting()
	originalFetchSetting := *fetchSetting
	fetchSetting.EnableSSRFProtection = true
	fetchSetting.AllowPrivateIp = false
	fetchSetting.DomainFilterMode = false
	fetchSetting.IpFilterMode = false
	fetchSetting.DomainList = nil
	fetchSetting.IpList = nil
	fetchSetting.AllowedPorts = []string{"443"}
	t.Cleanup(func() { *fetchSetting = originalFetchSetting })
	service.InitHttpClient()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/videos/:task_id/content/:filename", SignedVideoProxy)
	now := time.Now()

	channelURL := taskcommon.BuildSignedVideoProxyURLAt(now, 84, "task_channel_port", "clip.mp4")
	channelRequest := httptest.NewRequest(http.MethodGet, mustRequestURI(t, channelURL), nil)
	channelRecorder := httptest.NewRecorder()
	r.ServeHTTP(channelRecorder, channelRequest)
	assert.Equal(t, http.StatusOK, channelRecorder.Code)
	assert.Equal(t, "video-bytes", channelRecorder.Body.String())

	resultURL := taskcommon.BuildSignedVideoProxyURLAt(now, 84, "task_result_port", "clip.mp4")
	resultRequest := httptest.NewRequest(http.MethodGet, mustRequestURI(t, resultURL), nil)
	resultRecorder := httptest.NewRecorder()
	r.ServeHTTP(resultRecorder, resultRequest)
	assert.Equal(t, http.StatusForbidden, resultRecorder.Code)
	assert.Contains(t, resultRecorder.Body.String(), "port ")
	assert.Contains(t, resultRecorder.Body.String(), "is not allowed")
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
	r.HEAD("/v1/videos/:task_id/content/:filename", SignedVideoProxy)
	r.GET("/v1/video/generations/:task_id/content/:filename", SignedVideoProxy)
	r.HEAD("/v1/video/generations/:task_id/content/:filename", SignedVideoProxy)
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
