package relay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVolcengineDraftKeepsOwnershipAndSubmissionCredential(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = originalDB; _ = sqlDB.Close() })
	baseURL := "https://ai.artsapi.com"
	ch := &model.Channel{Id: 2, Type: constant.ChannelTypeMoziaArtsapi, Key: "rotated-key", BaseURL: &baseURL, Status: common.ChannelStatusEnabled}
	ch.ChannelInfo.IsMultiKey = true
	require.NoError(t, db.Create(ch).Error)
	require.NoError(t, db.Create(&model.Task{
		TaskID: "cgt-draft", UserId: 42, ChannelId: ch.Id,
		Platform:    constant.TaskPlatformVolcengineVideo,
		PrivateData: model.TaskPrivateData{Key: "submission-key"},
	}).Error)

	for _, tc := range []struct {
		userID int
		id     string
		valid  bool
	}{
		{42, "cgt-draft", true},
		{99, "cgt-draft", false},
		{42, "", false},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, constant.VolcengineVideoTaskPath, strings.NewReader(`{"model":"public-model","content":[{"type":"draft_task","draft_task":{"id":"`+tc.id+`"}}]}`))
		c.Request.Header.Set("Content-Type", "application/json")
		info := &relaycommon.RelayInfo{
			UserId: tc.userID, OriginModelName: "public-model",
			TaskRelayInfo: &relaycommon.TaskRelayInfo{},
			ChannelMeta:   &relaycommon.ChannelMeta{ChannelId: 1},
		}
		taskErr := ResolveOriginTask(c, info)
		if !tc.valid {
			require.NotNil(t, taskErr)
			assert.Nil(t, info.LockedChannel)
			continue
		}
		require.Nil(t, taskErr)
		assert.Equal(t, 2, info.ChannelId)
		assert.Equal(t, "submission-key", info.ApiKey)
		locked, ok := info.LockedChannel.(*model.Channel)
		require.True(t, ok)
		assert.False(t, locked.ChannelInfo.IsMultiKey)
		assert.Equal(t, "submission-key", locked.Key)
	}
	stored, err := model.GetChannelById(2, true)
	require.NoError(t, err)
	assert.Equal(t, "rotated-key", stored.Key, "routing must not modify channel configuration")
	assert.True(t, stored.ChannelInfo.IsMultiKey)
}
