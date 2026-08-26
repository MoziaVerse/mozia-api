package helper

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/mozia_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleGroupRatioUsesChannelFallbackAndModelPriority(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := mozia_setting.UserModelRatios2JSONString()
	t.Cleanup(func() {
		require.NoError(t, mozia_setting.UpdateUserModelRatiosByJSONString(original))
	})
	require.NoError(t, mozia_setting.UpdateUserModelRatiosByJSONString(
		`{"channel:396:35":{"user_id":396,"scope":"channel","channel_id":35,"ratio":0.5},"396:priority-model":{"user_id":396,"scope":"model","model":"priority-model","ratio":0.36}}`,
	))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(ctx, constant.ContextKeyChannelId, 35)

	channelInfo := HandleGroupRatio(ctx, &relaycommon.RelayInfo{
		UserId:          396,
		OriginModelName: "other-model",
		UserGroup:       "default",
		UsingGroup:      "default",
	})
	assert.True(t, channelInfo.HasUserModelRatio)
	assert.InDelta(t, 0.5, channelInfo.UserModelRatio, 1e-12)
	assert.InDelta(t, channelInfo.BaseGroupRatio*0.5, channelInfo.GroupRatio, 1e-12)

	modelInfo := HandleGroupRatio(ctx, &relaycommon.RelayInfo{
		UserId:          396,
		OriginModelName: "priority-model",
		UserGroup:       "default",
		UsingGroup:      "default",
	})
	assert.True(t, modelInfo.HasUserModelRatio)
	assert.InDelta(t, 0.36, modelInfo.UserModelRatio, 1e-12)
	assert.InDelta(t, modelInfo.BaseGroupRatio*0.36, modelInfo.GroupRatio, 1e-12)
}

func TestRefreshUserModelRatioUsesRetryChannelForFinalSettlement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	original := mozia_setting.UserModelRatios2JSONString()
	t.Cleanup(func() {
		require.NoError(t, mozia_setting.UpdateUserModelRatiosByJSONString(original))
	})
	require.NoError(t, mozia_setting.UpdateUserModelRatiosByJSONString(
		`{"channel:396:36":{"user_id":396,"scope":"channel","channel_id":36,"ratio":0.25}}`,
	))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		UserId:          396,
		OriginModelName: "retry-model",
		UserGroup:       "default",
		UsingGroup:      "default",
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelId: 36},
		PriceData: types.PriceData{
			GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1, BaseGroupRatio: 1},
		},
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			EstimatedQuotaBeforeGroup: 100,
			EstimatedQuotaAfterGroup:  100,
			GroupRatio:                1,
		},
	}

	RefreshUserModelRatio(ctx, info)
	assert.InDelta(t, 0.25, info.PriceData.GroupRatioInfo.UserModelRatio, 1e-12)
	assert.InDelta(t, info.PriceData.GroupRatioInfo.BaseGroupRatio*0.25, info.PriceData.GroupRatioInfo.GroupRatio, 1e-12)
	assert.InDelta(t, info.PriceData.GroupRatioInfo.GroupRatio, info.TieredBillingSnapshot.GroupRatio, 1e-12)
	assert.Equal(t, billingexpr.QuotaRound(100*info.PriceData.GroupRatioInfo.GroupRatio), info.TieredBillingSnapshot.EstimatedQuotaAfterGroup)
}

func TestModelPriceHelperTieredUsesPreloadedRequestInput(t *testing.T) {
	gin.SetMode(gin.TestMode)

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode":        `{"tiered-test-model":"tiered_expr"}`,
		"billing_setting.billing_expr":        `{"tiered-test-model":"param(\"stream\") == true ? tier(\"stream\", p * 3) : tier(\"base\", p * 2)"}`,
		mozia_setting.UserModelRatioOptionKey: `{"396:tiered-test-model":{"user_id":396,"model":"tiered-test-model","ratio":0.36}}`,
	}))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/channel/test/1", nil)
	req.Body = nil
	req.ContentLength = 0
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	ctx.Set("group", "default")

	info := &relaycommon.RelayInfo{
		UserId:          396,
		OriginModelName: "tiered-test-model",
		UserGroup:       "default",
		UsingGroup:      "default",
		RequestHeaders:  map[string]string{"Content-Type": "application/json"},
		BillingRequestInput: &billingexpr.RequestInput{
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"stream":true}`),
		},
	}

	priceData, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.Equal(t, 540, priceData.QuotaToPreConsume)
	assert.Equal(t, 1.0, priceData.GroupRatioInfo.BaseGroupRatio)
	assert.Equal(t, 0.36, priceData.GroupRatioInfo.UserModelRatio)
	assert.Equal(t, 0.36, priceData.GroupRatioInfo.GroupRatio)
	require.NotNil(t, info.TieredBillingSnapshot)
	require.Equal(t, 0.36, info.TieredBillingSnapshot.GroupRatio)
	require.Equal(t, "stream", info.TieredBillingSnapshot.EstimatedTier)
	require.Equal(t, billing_setting.BillingModeTieredExpr, info.TieredBillingSnapshot.BillingMode)
	require.Equal(t, common.QuotaPerUnit, info.TieredBillingSnapshot.QuotaPerUnit)
}

func TestModelPriceHelpersApplyUserModelRatioToRatioAndFixedPricing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	original := mozia_setting.UserModelRatios2JSONString()
	originalModelRatios := ratio_setting.ModelRatio2JSONString()
	originalModelPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, mozia_setting.UpdateUserModelRatiosByJSONString(original))
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalModelPrices))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"user-ratio-test-model":2}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"user-fixed-test-model":0.1}`))
	require.NoError(t, mozia_setting.UpdateUserModelRatiosByJSONString(
		`{"396:user-ratio-test-model":{"user_id":396,"model":"user-ratio-test-model","ratio":0.36},"396:user-fixed-test-model":{"user_id":396,"model":"user-fixed-test-model","ratio":0.36}}`,
	))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	ratioInfo := &relaycommon.RelayInfo{
		UserId:          396,
		OriginModelName: "user-ratio-test-model",
		UserGroup:       "default",
		UsingGroup:      "default",
	}
	ratioPrice, err := ModelPriceHelper(ctx, ratioInfo, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	modelRatio, ok, _ := ratio_setting.GetModelRatio("user-ratio-test-model")
	require.True(t, ok)
	preConsumedTokens := common.Max(1000, common.PreConsumedQuota)
	assert.Equal(t, int(float64(preConsumedTokens)*modelRatio*0.36), ratioPrice.QuotaToPreConsume)
	assert.True(t, ratioPrice.GroupRatioInfo.HasUserModelRatio)

	fixedInfo := &relaycommon.RelayInfo{
		UserId:          396,
		OriginModelName: "user-fixed-test-model",
		UserGroup:       "default",
		UsingGroup:      "default",
	}
	fixedPrice, err := ModelPriceHelperPerCall(ctx, fixedInfo)
	require.NoError(t, err)
	modelPrice, ok := ratio_setting.GetModelPrice("user-fixed-test-model", true)
	require.True(t, ok)
	assert.Equal(t, int(modelPrice*common.QuotaPerUnit*0.36), fixedPrice.Quota)
	assert.True(t, fixedPrice.GroupRatioInfo.HasUserModelRatio)
}

func TestModelPriceHelperPerCallUsesReferenceVideoPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalPrices := ratio_setting.ModelPrice2JSONString()
	originalReferencePrices := ratio_setting.ReferenceVideoPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalPrices))
		require.NoError(t, ratio_setting.UpdateReferenceVideoPriceByJSONString(originalReferencePrices))
	})
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"dual-price-video-model":0.1}`))
	require.NoError(t, ratio_setting.UpdateReferenceVideoPriceByJSONString(`{"dual-price-video-model":0.2}`))

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(`{"reference_video":"https://example.com/ref.mp4"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	priceData, err := ModelPriceHelperPerCall(ctx, &relaycommon.RelayInfo{
		OriginModelName: "dual-price-video-model",
		UserGroup:       "default",
		UsingGroup:      "default",
	})

	require.NoError(t, err)
	assert.Equal(t, 0.2, priceData.ModelPrice)
	assert.Equal(t, int(0.2*common.QuotaPerUnit), priceData.Quota)
}
