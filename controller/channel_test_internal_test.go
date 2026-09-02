package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSettleTestQuotaUsesTieredBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:   "tiered_expr",
			ExprString:    `param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`,
			ExprHash:      billingexpr.ExprHashString(`param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`),
			GroupRatio:    1,
			EstimatedTier: "stream",
			QuotaPerUnit:  common.QuotaPerUnit,
			ExprVersion:   1,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	quota, result := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestBuildTestLogOtherInjectsTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	priceData := types.PriceData{
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	other := buildTestLogOther(ctx, info, priceData, usage, &billingexpr.TieredResult{
		MatchedTier: "base",
	})

	require.Equal(t, "tiered_expr", other["billing_mode"])
	require.Equal(t, "base", other["matched_tier"])
	require.NotEmpty(t, other["expr_b64"])
}

func TestResolveChannelTestUserIDUsesRequestUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 2)

	userID, err := resolveChannelTestUserID(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, userID)
}

func TestCompatibleVideoChannelsSkipChatCompletionTest(t *testing.T) {
	for _, channelType := range []int{
		constant.ChannelTypeMoziaSeedanceGen,
		constant.ChannelTypeMoziaSeedanceVideos,
		constant.ChannelTypeMoziaArtsapi,
		constant.ChannelTypeMoziaH3,
	} {
		result := testChannel(nil, &model.Channel{Type: channelType}, 0, "", "", false)

		require.Error(t, result.localErr)
		require.Contains(t, result.localErr.Error(), "channel test is not supported")
	}
}

func TestGlobalaiopcChannelSkipsChatCompletionTest(t *testing.T) {
	result := testChannel(nil, &model.Channel{Type: constant.ChannelTypeMoziaGlobalaiopc}, 0, "", "", false)

	require.Error(t, result.localErr)
	require.Contains(t, result.localErr.Error(), "channel test is not supported")
}

func TestSelectChannelsForAutomaticTestPassiveRecoveryOnlyUsesAutoDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModePassiveRecovery)

	require.Len(t, selected, 1)
	require.Equal(t, 2, selected[0].Id)
}

func TestSelectChannelsForAutomaticTestScheduledSkipsManualDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeScheduledAll)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Id)
	require.Equal(t, 2, selected[1].Id)
}

func TestCopyChannelCanStartDisabled(t *testing.T) {
	tests := []struct {
		name            string
		query           string
		expectedStatus  int
		expectedAbility bool
	}{
		{name: "default remains enabled", expectedStatus: common.ChannelStatusEnabled, expectedAbility: true},
		{name: "explicitly disabled", query: "?disabled=true", expectedStatus: common.ChannelStatusManuallyDisabled, expectedAbility: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := setupModelListControllerTestDB(t)
			require.NoError(t, db.AutoMigrate(&model.Log{}))

			origin := &model.Channel{
				Name:      "upstream models",
				Key:       "secret-key",
				Status:    common.ChannelStatusEnabled,
				Models:    "vendor/model-a,vendor/model-b",
				Group:     "default",
				Balance:   12,
				UsedQuota: 34,
			}
			require.NoError(t, db.Create(origin).Error)

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/channel/copy/%d%s", origin.Id, tt.query), nil)
			ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprint(origin.Id)}}

			CopyChannel(ctx)

			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Success bool `json:"success"`
				Data    struct {
					Id int `json:"id"`
				} `json:"data"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.True(t, response.Success)

			clone, err := model.GetChannelById(response.Data.Id, true)
			require.NoError(t, err)
			require.Equal(t, tt.expectedStatus, clone.Status)
			require.Equal(t, origin.Key, clone.Key)
			require.Equal(t, origin.Models, clone.Models)
			require.Equal(t, origin.Name+"_复制", clone.Name)
			require.Zero(t, clone.Balance)
			require.Zero(t, clone.UsedQuota)

			var abilities []model.Ability
			require.NoError(t, db.Where("channel_id = ?", clone.Id).Order("model ASC").Find(&abilities).Error)
			require.Len(t, abilities, 2)
			require.Equal(t, tt.expectedAbility, abilities[0].Enabled)
			require.Equal(t, tt.expectedAbility, abilities[1].Enabled)
		})
	}
}

func TestCopyChannelCanCreateMaterialOnlyChannel(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	origin := &model.Channel{
		Name:      "Cool models",
		Type:      constant.ChannelTypeMoziaCool,
		Key:       "secret-key",
		Status:    common.ChannelStatusEnabled,
		Models:    "video-a,video-b",
		Group:     "default",
		AutoBan:   common.GetPointer(1),
		TestModel: common.GetPointer("video-a"),
	}
	require.NoError(t, origin.Insert())

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/channel/copy/%d?material_only=true&name=material-upload", origin.Id), nil)
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprint(origin.Id)}}

	CopyChannel(ctx)

	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Id int `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	clone, err := model.GetChannelById(response.Data.Id, true)
	require.NoError(t, err)
	require.Equal(t, "material-upload", clone.Name)
	require.Empty(t, clone.Models)
	require.Nil(t, clone.TestModel)
	require.False(t, clone.GetAutoBan())

	var abilityCount int64
	require.NoError(t, db.Model(&model.Ability{}).Where("channel_id = ?", clone.Id).Count(&abilityCount).Error)
	require.Zero(t, abilityCount)
}

func TestTestAllChannelsRejectsExistingActiveTask(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))

	existing, err := model.CreateSystemTask(model.SystemTaskTypeChannelTest, nil, nil)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/test", nil)

	TestAllChannels(ctx)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), existing.TaskID)
	require.Contains(t, recorder.Body.String(), "已有通道测试任务正在运行或等待中")
}
