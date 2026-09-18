package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/mozia_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestDistributePreservesPublicModelAcrossMappingsAndRetries(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/channels.db"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	previousDB := model.DB
	previousRules := mozia_setting.UserModelRedirects2JSONString()
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		require.NoError(t, sqlDB.Close())
		require.NoError(t, mozia_setting.UpdateUserModelRedirectsByJSONString(previousRules))
	})
	require.NoError(t, db.AutoMigrate(&model.Channel{}))
	require.NoError(t, mozia_setting.UpdateUserModelRedirectsByJSONString(`{"redirect":{"id":"redirect","all_users":true,"source_model":"public/redirected","target_model":"effective-model","seamless":false}}`))
	channels := []model.Channel{
		{Id: 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Key: "test-key", ModelMapping: common.GetPointer(`{"public/k3":"kimi-k3-fireworks","effective-model":"kimi-k3-fireworks"}`)},
		{Id: 2, Type: constant.ChannelTypeDeepSeek, Status: common.ChannelStatusEnabled, Key: "test-key", ModelMapping: common.GetPointer(`{"public/k3":"second-upstream","effective-model":"second-upstream"}`)},
	}
	require.NoError(t, db.Create(&channels).Error)
	for _, publicModel := range []string{"public/k3", "public/redirected"} {
		for _, channelID := range []int{1, 2} {
			t.Run(publicModel+"/channel"+strconv.Itoa(channelID), func(t *testing.T) {
				r := gin.New()
				r.POST("/v1/chat/completions", func(c *gin.Context) {
					common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, strconv.Itoa(channelID))
				}, Distribute(), func(c *gin.Context) {
					effective := publicModel
					if publicModel == "public/redirected" {
						effective = "effective-model"
					}
					assert.Equal(t, effective, common.GetContextKeyString(c, constant.ContextKeyOriginalModel))
					// Exercise the selected channel and a subsequent retry on the other channel.
					for _, id := range []int{channelID, 3 - channelID} {
						require.Nil(t, SetupContextForSelectedChannel(c, &channels[id-1], effective))
						info := &relaycommon.RelayInfo{OriginModelName: effective}
						info.InitChannelMeta(c)
						request := &dto.GeneralOpenAIRequest{Model: effective}
						require.NoError(t, helper.ModelMappedHelper(c, info, request))
						expectedUpstream := "kimi-k3-fireworks"
						if id == 2 {
							expectedUpstream = "second-upstream"
						}
						assert.Equal(t, expectedUpstream, request.Model, "upstream mapping must remain intact")
						assert.Equal(t, effective, info.OriginModelName, "billing model must remain intact")
						assert.Equal(t, publicModel, common.GetUserVisibleModel(c, ""))
					}
					service.IOCopyBytesGracefully(c, nil, []byte(`{"model":"second-upstream"}`))
				})
				rec := httptest.NewRecorder()
				request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"`+publicModel+`","messages":[]}`))
				request.Header.Set("Content-Type", "application/json")
				r.ServeHTTP(rec, request)
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				assert.JSONEq(t, `{"model":"`+publicModel+`"}`, rec.Body.String())
			})
		}
	}
}
