package model

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestUpdateModelPricingOptionsPreservesOtherModels(t *testing.T) {
	originalDB := DB
	originalModelPrice := ratio_setting.ModelPrice2JSONString()
	common.OptionMapRWMutex.Lock()
	originalOptionMap := common.OptionMap
	common.OptionMap = map[string]string{"ModelPrice": `{"target":1,"other":2}`}
	common.OptionMapRWMutex.Unlock()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&Option{}))
	DB = db
	t.Cleanup(func() {
		DB = originalDB
		_ = updateOptionMap("ModelPrice", originalModelPrice)
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptionMap
		common.OptionMapRWMutex.Unlock()
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	require.NoError(t, UpdateModelPricingOptions("target", map[string]json.RawMessage{
		"ModelPrice": json.RawMessage(`3.5`),
	}))

	var option Option
	require.NoError(t, db.First(&option, "key = ?", "ModelPrice").Error)
	assert.JSONEq(t, `{"target":3.5,"other":2}`, option.Value)

	require.NoError(t, UpdateModelPricingOptions("target", map[string]json.RawMessage{
		"ModelPrice": json.RawMessage(`null`),
	}))
	require.NoError(t, db.First(&option, "key = ?", "ModelPrice").Error)
	assert.JSONEq(t, `{"other":2}`, option.Value)
}
