package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/mozia_setting"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type moziaUserModelRatioResponse struct {
	Success bool                           `json:"success"`
	Message string                         `json:"message"`
	Data    []mozia_setting.UserModelRatio `json:"data"`
}

type moziaUserModelRatioItemResponse struct {
	Success bool                         `json:"success"`
	Message string                       `json:"message"`
	Data    mozia_setting.UserModelRatio `json:"data"`
}

func setupMoziaUserModelRatioControllerTest(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalRatios := mozia_setting.UserModelRatios2JSONString()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Option{}))
	model.DB = db
	model.LOG_DB = db
	model.InitOptionMap()

	t.Cleanup(func() {
		require.NoError(t, mozia_setting.UpdateUserModelRatiosByJSONString(originalRatios))
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func performMoziaUserModelRatioRequest(
	t *testing.T,
	method string,
	target string,
	body string,
	params gin.Params,
	handler gin.HandlerFunc,
) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = params
	handler(ctx)
	return recorder
}

func TestMoziaUserModelRatioCRUDPersistsOptionAndSupportsSlashModel(t *testing.T) {
	db := setupMoziaUserModelRatioControllerTest(t)
	require.NoError(t, db.Create(&model.User{
		Id:       396,
		Username: "ratio-user",
		Password: "password",
		Group:    "default",
	}).Error)

	recorder := performMoziaUserModelRatioRequest(
		t,
		http.MethodPost,
		"/api/mozia/user-model-ratio",
		`{"user_id":396,"model":"vendor/video-v1","ratio":0.36}`,
		nil,
		UpsertMoziaUserModelRatio,
	)
	var itemResponse moziaUserModelRatioItemResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &itemResponse))
	require.True(t, itemResponse.Success, itemResponse.Message)
	assert.Equal(t, mozia_setting.UserModelRatio{
		UserId: 396,
		Model:  "vendor/video-v1",
		Ratio:  0.36,
	}, itemResponse.Data)

	var option model.Option
	require.NoError(t, db.First(&option, "key = ?", mozia_setting.UserModelRatioOptionKey).Error)
	assert.Contains(t, option.Value, `"vendor/video-v1"`)
	ratio, ok := mozia_setting.GetUserModelRatio(396, "vendor/video-v1")
	require.True(t, ok)
	assert.InDelta(t, 0.36, ratio, 1e-12)

	recorder = performMoziaUserModelRatioRequest(
		t,
		http.MethodGet,
		"/api/mozia/user-model-ratio",
		"",
		nil,
		GetMoziaUserModelRatios,
	)
	var listResponse moziaUserModelRatioResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &listResponse))
	require.True(t, listResponse.Success, listResponse.Message)
	assert.Equal(t, []mozia_setting.UserModelRatio{{
		UserId: 396,
		Model:  "vendor/video-v1",
		Ratio:  0.36,
	}}, listResponse.Data)

	recorder = performMoziaUserModelRatioRequest(
		t,
		http.MethodDelete,
		"/api/mozia/user-model-ratio/396?model=vendor%2Fvideo-v1",
		"",
		gin.Params{{Key: "user_id", Value: "396"}},
		DeleteMoziaUserModelRatio,
	)
	var deleteResponse struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &deleteResponse))
	require.True(t, deleteResponse.Success, deleteResponse.Message)
	_, ok = mozia_setting.GetUserModelRatio(396, "vendor/video-v1")
	assert.False(t, ok)
}

func TestMoziaUserModelRatioRejectsMissingUserAndNonPositiveRatio(t *testing.T) {
	db := setupMoziaUserModelRatioControllerTest(t)

	recorder := performMoziaUserModelRatioRequest(
		t,
		http.MethodPost,
		"/api/mozia/user-model-ratio",
		`{"user_id":999,"model":"video-v1","ratio":0.36}`,
		nil,
		UpsertMoziaUserModelRatio,
	)
	var response moziaUserModelRatioItemResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Contains(t, response.Message, "用户不存在")

	require.NoError(t, db.Create(&model.User{
		Id:       396,
		Username: "ratio-user",
		Password: "password",
		Group:    "default",
	}).Error)
	recorder = performMoziaUserModelRatioRequest(
		t,
		http.MethodPost,
		"/api/mozia/user-model-ratio",
		`{"user_id":396,"model":"video-v1","ratio":0}`,
		nil,
		UpsertMoziaUserModelRatio,
	)
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Contains(t, response.Message, "greater than 0")
}
