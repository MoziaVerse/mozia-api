package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type moziaWalletAuditOther struct {
	AdminInfo struct {
		AdminID       int    `json:"admin_id"`
		AdminUsername string `json:"admin_username"`
	} `json:"admin_info"`
	Op struct {
		Action string `json:"action"`
		Params struct {
			TargetUserID        int    `json:"target_user_id"`
			Source              string `json:"source"`
			Delta               *int   `json:"delta"`
			TargetBalance       *int   `json:"target_balance"`
			BalanceAfter        int    `json:"balance_after"`
			BalanceAfterDisplay string `json:"balance_after_display"`
			Quota               string `json:"quota"`
			Reason              string `json:"reason"`
		} `json:"params"`
	} `json:"op"`
}

func setupMoziaWalletAuditControllerTest(t *testing.T) *gorm.DB {
	t.Helper()
	gin.SetMode(gin.TestMode)

	originalDB := model.DB
	originalLogDB := model.LOG_DB
	originalMainDatabaseType := common.MainDatabaseType()
	originalLogDatabaseType := common.LogDatabaseType()
	originalRedisEnabled := common.RedisEnabled

	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	common.RedisEnabled = false
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{},
		&model.Log{},
		&model.MoziaWalletBalance{},
		&model.MoziaWalletTransaction{},
	))
	model.DB = db
	model.LOG_DB = db

	t.Cleanup(func() {
		model.DB = originalDB
		model.LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainDatabaseType, originalLogDatabaseType)
		common.RedisEnabled = originalRedisEnabled
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func adjustMoziaWalletForTest(t *testing.T, body string, targetUserID int) struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
} {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/mozia/wallet", strings.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Params = gin.Params{{Key: "id", Value: fmt.Sprintf("%d", targetUserID)}}
	ctx.Set("id", 1)
	ctx.Set("username", "root")
	ctx.Set("role", common.RoleRootUser)
	AdjustMoziaUserWallet(ctx)

	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}

func TestAdjustMoziaUserWalletRecordsVisibleManageAudit(t *testing.T) {
	db := setupMoziaWalletAuditControllerTest(t)
	require.NoError(t, db.Create(&model.User{
		Id:       1,
		Username: "root",
		Password: "password",
		Role:     common.RoleRootUser,
		Status:   common.UserStatusEnabled,
		AffCode:  "root-audit",
	}).Error)
	require.NoError(t, db.Create(&model.User{
		Id:       2,
		Username: "wallet-user",
		Password: "password",
		Quota:    100,
		Status:   common.UserStatusEnabled,
		AffCode:  "wallet-audit",
	}).Error)
	require.NoError(t, model.RecordMoziaInitialGiftQuota(2, 100, "test", "initial"))

	requests := []string{
		`{"source":"gift","delta":25,"reason":"support credit"}`,
		`{"source":"gift","delta":-5,"reason":"support debit"}`,
		`{"source":"paid","target_balance":20,"reason":"paid correction"}`,
	}
	for _, body := range requests {
		response := adjustMoziaWalletForTest(t, body, 2)
		require.True(t, response.Success, response.Message)
	}

	var logs []model.Log
	require.NoError(t, db.Where("type = ?", model.LogTypeManage).Order("id").Find(&logs).Error)
	require.Len(t, logs, 3)

	expectedActions := []string{
		"mozia.wallet_balance_add",
		"mozia.wallet_balance_subtract",
		"mozia.wallet_balance_set",
	}
	expectedSources := []string{"gift", "gift", "paid"}
	expectedBalances := []int{125, 120, 20}
	expectedReasons := []string{"support credit", "support debit", "paid correction"}
	for i, log := range logs {
		assert.Equal(t, 1, log.UserId)
		assert.Equal(t, "root", log.Username)
		assert.Contains(t, log.Content, expectedReasons[i])

		var other moziaWalletAuditOther
		require.NoError(t, common.UnmarshalJsonStr(log.Other, &other))
		assert.Equal(t, 1, other.AdminInfo.AdminID)
		assert.Equal(t, "root", other.AdminInfo.AdminUsername)
		assert.Equal(t, expectedActions[i], other.Op.Action)
		assert.Equal(t, 2, other.Op.Params.TargetUserID)
		assert.Equal(t, expectedSources[i], other.Op.Params.Source)
		assert.Equal(t, expectedBalances[i], other.Op.Params.BalanceAfter)
		assert.NotEmpty(t, other.Op.Params.BalanceAfterDisplay)
		assert.Equal(t, expectedReasons[i], other.Op.Params.Reason)
	}

	var firstOther moziaWalletAuditOther
	require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &firstOther))
	require.NotNil(t, firstOther.Op.Params.Delta)
	assert.Equal(t, 25, *firstOther.Op.Params.Delta)
	assert.NotEmpty(t, firstOther.Op.Params.Quota)

	visibleLogs, total, err := model.GetAllLogs(model.LogTypeManage, 0, 0, "", "", "", 0, 10, 0, "", "", "")
	require.NoError(t, err)
	assert.EqualValues(t, 3, total)
	assert.Len(t, visibleLogs, 3)

	noOp := adjustMoziaWalletForTest(t, `{"source":"gift","delta":0,"reason":"no-op"}`, 2)
	assert.True(t, noOp.Success)
	failed := adjustMoziaWalletForTest(t, `{"source":"gift","delta":-1000,"reason":"must fail"}`, 2)
	assert.False(t, failed.Success)
	var logCount int64
	require.NoError(t, db.Model(&model.Log{}).Where("type = ?", model.LogTypeManage).Count(&logCount).Error)
	assert.EqualValues(t, 3, logCount)
}
