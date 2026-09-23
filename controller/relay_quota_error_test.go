package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRelayWalletInsufficientReturns403AndRecordsError(t *testing.T) {
	db := setupMoziaWalletAuditControllerTest(t)
	require.NoError(t, db.AutoMigrate(&model.MoziaWalletReservation{}, &model.MoziaModelQuotaPolicy{},
		&model.Token{}, &model.UserSSO{}, &model.Reseller{}, &model.ResellerCustomer{}))
	oldLog, oldCount, oldSensitive, oldBatch := constant.ErrorLogEnabled, constant.CountToken, setting.CheckSensitiveEnabled, common.BatchUpdateEnabled
	oldPrices, oldOptions := ratio_setting.ModelPrice2JSONString(), common.OptionMap
	constant.ErrorLogEnabled, constant.CountToken, setting.CheckSensitiveEnabled, common.BatchUpdateEnabled = true, false, false, false
	common.OptionMap = map[string]string{}
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"quota-test-model":0.001}`))
	t.Cleanup(func() {
		constant.ErrorLogEnabled, constant.CountToken, setting.CheckSensitiveEnabled, common.BatchUpdateEnabled = oldLog, oldCount, oldSensitive, oldBatch
		common.OptionMap = oldOptions
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
	})
	require.NoError(t, db.Create(&model.User{Id: 91, Username: "quota-test", Quota: 1000, Status: common.UserStatusEnabled}).Error)
	require.NoError(t, model.RecordMoziaInitialGiftQuota(91, 1000, "test", "gift"))
	require.NoError(t, model.GrantMoziaWalletQuota(model.MoziaWalletGrantInput{
		UserId: 91, Source: model.MoziaWalletSourcePaid, Amount: 1,
		EventType: model.MoziaWalletEventTopUp, ReferenceType: "test", ReferenceId: "paid",
	}))
	require.NoError(t, model.CreateMoziaModelQuotaPolicy(&model.MoziaModelQuotaPolicy{
		ModelPattern: "quota-test-model", MatchType: model.MoziaQuotaPolicyMatchExact,
		AllowedSources: model.MoziaWalletSourcePaid, ConsumeOrder: model.MoziaQuotaPolicyConsumePaidFirst, Enabled: true,
	}))
	require.NoError(t, db.Create(&model.Token{Id: 92, UserId: 91, Key: "test-key", RemainQuota: 10000}).Error)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"quota-test-model","messages":[{"role":"user","content":"hello"}]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("id", 91)
	c.Set("original_model", "quota-test-model")
	c.Set("group", "default")
	c.Set("user_group", "default")
	c.Set("user_setting", dto.UserSetting{BillingPreference: "wallet_only"})
	c.Set("token_id", 92)
	c.Set("token_key", "test-key")
	c.Set(common.RequestIdKey, "quota-http-request")

	Relay(c, types.RelayFormatOpenAI)
	require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
	var response struct {
		Error types.OpenAIError `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, "insufficient_user_quota", response.Error.Code)
	assert.Contains(t, response.Error.Message, "quota-http-request")
	var logs []model.Log
	require.NoError(t, db.Where("request_id = ?", "quota-http-request").Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Zero(t, logs[0].ChannelId)
	assert.Equal(t, model.LogTypeError, logs[0].Type)
	assert.Empty(t, logs[0].UpstreamRequestId)
}

func TestTaskQuotaErrorKeepsLocal403(t *testing.T) {
	apiErr := types.NewErrorWithStatusCode(model.ErrMoziaWalletInsufficient, types.ErrorCodeInsufficientUserQuota,
		http.StatusForbidden, types.ErrOptionWithSkipRetry())
	taskErr := service.TaskErrorFromAPIError(apiErr)
	require.True(t, taskErr.LocalError, "billing failures must not enter channel error logging")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	assert.False(t, shouldRetryTaskRelay(c, 1, taskErr, 3))
	respondTaskError(c, taskErr)
	assert.Equal(t, http.StatusForbidden, recorder.Code)
	assert.JSONEq(t, `{"code":"insufficient_user_quota","message":"mozia wallet quota insufficient","type":"permission_error","data":null}`, recorder.Body.String())
}

func TestQuotaLogsDistinguishLocalAndUpstreamErrors(t *testing.T) {
	db := setupMoziaWalletAuditControllerTest(t)
	oldLog := constant.ErrorLogEnabled
	constant.ErrorLogEnabled = true
	t.Cleanup(func() { constant.ErrorLogEnabled = oldLog })
	require.NoError(t, db.Create(&model.User{Id: 93, Username: "quota-log-test"}).Error)
	for _, upstream := range []bool{false, true} {
		apiErr := types.NewErrorWithStatusCode(model.ErrMoziaWalletInsufficient, types.ErrorCodeInsufficientUserQuota,
			http.StatusForbidden, types.ErrOptionWithSkipRetry())
		wantChannel := 0
		requestID := "local-quota-error"
		if upstream {
			apiErr = types.WithOpenAIError(types.OpenAIError{Code: "insufficient_user_quota", Message: "upstream quota insufficient"}, http.StatusForbidden)
			wantChannel, requestID = 99, "upstream-quota-error"
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("id", 93)
		c.Set("channel_id", 99)
		c.Set("original_model", "quota-test-model")
		c.Set(common.RequestIdKey, requestID)
		processChannelError(c, types.ChannelError{ChannelId: 99}, apiErr)
		var logs []model.Log
		require.NoError(t, db.Where("request_id = ?", requestID).Find(&logs).Error)
		require.Len(t, logs, 1)
		assert.Equal(t, wantChannel, logs[0].ChannelId)
	}
}
