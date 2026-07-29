package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupMaterialServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

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

func TestResolveMaterialUpstreamUsesMatchedGroupAndSkipsEmptyKey(t *testing.T) {
	db := setupMaterialServiceTestDB(t)

	highPriority := int64(100)
	lowPriority := int64(10)
	baseURL := "https://material.internal"

	require.NoError(t, db.Create(&model.Channel{
		Id:       1,
		Name:     "vip-material",
		Type:     constant.ChannelTypeMoziaCool,
		Key:      "vip-key",
		Status:   common.ChannelStatusEnabled,
		Group:    "vip",
		Priority: &highPriority,
		BaseURL:  &baseURL,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:       2,
		Name:     "default-empty",
		Type:     constant.ChannelTypeMoziaCool,
		Key:      "",
		Status:   common.ChannelStatusEnabled,
		Group:    "default",
		Priority: &highPriority,
		BaseURL:  &baseURL,
	}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id:       3,
		Name:     "default-valid",
		Type:     constant.ChannelTypeMoziaCool,
		Key:      "usable-key",
		Status:   common.ChannelStatusEnabled,
		Group:    "default,team",
		Priority: &lowPriority,
		BaseURL:  &baseURL,
	}).Error)

	upstream, err := ResolveMaterialUpstream("", "default")

	require.NoError(t, err)
	require.NotNil(t, upstream)
	assert.Equal(t, 3, upstream.ChannelID)
	assert.Equal(t, "usable-key", upstream.APIKey)
	assert.Equal(t, baseURL, upstream.BaseURL)
}
