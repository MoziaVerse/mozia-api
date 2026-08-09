package relay

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMidjourneySubmitUsesSharedResellerBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainType := common.MainDatabaseType()
	originalLogType := common.LogDatabaseType()
	originalQuotaPerUnit := common.QuotaPerUnit
	originalRatios := ratio_setting.ModelRatio2JSONString()
	originalPrices := ratio_setting.ModelPrice2JSONString()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.UserSSO{}, &model.Reseller{}, &model.ResellerCustomer{},
		&model.ResellerPriceRule{}, &model.ResellerRequestSettlement{},
		&model.MoziaWalletBalance{}, &model.MoziaWalletTransaction{}, &model.MoziaWalletReservation{},
		&model.MoziaModelQuotaPolicy{}, &model.Midjourney{}, &model.Log{}, &model.Channel{},
	))
	model.DB = db
	model.LOG_DB = db
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	common.QuotaPerUnit = 1_000
	service.InitHttpClient()
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"mj_imagine":0.1}`))
	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainType, originalLogType)
		common.QuotaPerUnit = originalQuotaPerUnit
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalPrices))
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	const userID, channelID, initialQuota = 71, 71, 1_000
	user := model.User{Id: userID, Username: "mj-reseller-user", Password: "test", Group: "default", Status: common.UserStatusEnabled, Quota: initialQuota, AffCode: "mj-reseller-aff"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, model.RecordMoziaInitialGiftQuota(userID, initialQuota, "test", "mj-reseller"))
	subject := "mj-reseller-subject"
	require.NoError(t, db.Create(&model.UserSSO{SSOSub: subject, UserId: userID}).Error)
	reseller := model.Reseller{Name: "MJ Reseller", Status: model.ResellerStatusActive}
	require.NoError(t, db.Create(&reseller).Error)
	customer := model.ResellerCustomer{ResellerId: reseller.Id, Subject: subject, Status: model.ResellerCustomerStatusActive}
	require.NoError(t, db.Create(&customer).Error)
	zero := 0
	_, err = model.CreateResellerPriceRule(model.CreateResellerPriceRuleParams{
		ResellerId: reseller.Id, Kind: model.ResellerPriceRuleKindWholesale, ModelName: "mj_imagine",
		MultiplierPPM: 800_000, ExpectedVersion: &zero, Enabled: true,
		EffectiveAt: common.GetTimestamp(), CreatedBy: "test",
	})
	require.NoError(t, err)
	_, err = model.CreateResellerPriceRule(model.CreateResellerPriceRuleParams{
		ResellerId: reseller.Id, Kind: model.ResellerPriceRuleKindRetail, ModelName: "mj_imagine",
		MultiplierPPM: 1_500_000, ExpectedVersion: &zero, Enabled: true,
		EffectiveAt: common.GetTimestamp(), CreatedBy: "test",
	})
	require.NoError(t, err)
	require.NoError(t, db.Create(&model.Channel{Id: channelID, Name: "mj-test", Key: "mj-secret", Status: common.ChannelStatusEnabled}).Error)

	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		assert.Equal(t, "/mj/submit/imagine", request.URL.Path)
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":1,"description":"ok","result":"mj-task-71"}`))
	}))
	defer upstream.Close()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/mj/submit/imagine", strings.NewReader(`{"prompt":"a moonlit harbor"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("base_url", upstream.URL)
	ctx.Set(string(constant.ContextKeyChannelId), channelID)
	ctx.Set(string(constant.ContextKeyChannelKey), "mj-secret")
	info := &relaycommon.RelayInfo{
		UserId: userID, RequestId: "mj-reseller-request_71", OriginModelName: "mj_imagine",
		UsingGroup: "default", UserGroup: "default", RelayMode: relayconstant.RelayModeMidjourneyImagine,
		StartTime: time.Now(), IsPlayground: true, UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
	}

	response := RelayMidjourneySubmit(ctx, info)
	require.Nil(t, response)
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"result":"mj-task-71"`)
	assert.Equal(t, initialQuota-150, mustUserQuota(t, userID))
	settlement, err := model.GetResellerRequestSettlement(info.RequestId)
	require.NoError(t, err)
	assert.Equal(t, model.ResellerSettlementStatusSettled, settlement.Status)
	assert.Equal(t, int64(100), settlement.ActualBaseQuota)
	assert.Equal(t, int64(150), settlement.ActualCustomerQuota)
	assert.Equal(t, int64(80), settlement.ActualWholesaleQuota)
	var storedTask model.Midjourney
	require.NoError(t, db.Where("mj_id = ?", "mj-task-71").Take(&storedTask).Error)
	assert.Equal(t, 100, storedTask.Quota, "task operational quota stays at the upstream base amount")
	var storedUser model.User
	require.NoError(t, db.Select("used_quota").Where("id = ?", userID).Take(&storedUser).Error)
	assert.Equal(t, 150, storedUser.UsedQuota)
	var storedChannel model.Channel
	require.NoError(t, db.Select("used_quota").Where("id = ?", channelID).Take(&storedChannel).Error)
	assert.Equal(t, int64(100), storedChannel.UsedQuota)
}

func mustUserQuota(t *testing.T, userID int) int {
	t.Helper()
	quota, err := model.GetUserQuota(userID, false)
	require.NoError(t, err)
	return quota
}
