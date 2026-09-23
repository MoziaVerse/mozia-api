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

func TestSSOMoziaWalletHistory(t *testing.T) {
	db := setupMoziaWalletAuditControllerTest(t)
	require.NoError(t, db.Create(&model.User{Id: 2, Username: "history-user"}).Error)
	for _, body := range []string{
		`{"source":"paid","delta":100,"reason":"private support ticket","public_note":"线下付款补登"}`,
		`{"source":"paid","delta":-20,"reason":"private correction"}`,
		`{"source":"paid","target_balance":90,"public_note":"余额更正"}`,
		`{"source":"paid","delta":0}`,
	} {
		response := adjustMoziaWalletForTest(t, body, 2)
		require.True(t, response.Success, response.Message)
	}
	failed := adjustMoziaWalletForTest(t, `{"source":"paid","delta":-1000}`, 2)
	require.False(t, failed.Success)
	tooLong := adjustMoziaWalletForTest(t, `{"source":"paid","delta":1,"public_note":"`+strings.Repeat("字", 501)+`"}`, 2)
	require.False(t, tooLong.Success)
	// These rows must not appear in this customer's receipt history.
	for _, row := range []model.MoziaWalletTransaction{
		{UserId: 3, Source: "paid", Delta: 999, EventType: "adjust", Metadata: `{"public_note":"other customer"}`},
		{UserId: 2, Source: "paid", Delta: -10, EventType: "consume"},
		{UserId: 2, Source: "paid", Delta: 10, EventType: "refund", ReferenceType: "reservation"},
		{UserId: 2, Source: "legacy", Delta: 100, EventType: "legacy_sync"},
	} {
		require.NoError(t, db.Create(&row).Error)
	}
	read := func(userId int, query string) (bool, model.MoziaWalletHistory, string) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Request = httptest.NewRequest(http.MethodGet, "/api/sso/user/wallet/history"+query, nil)
		ctx.Set("id", userId)
		GetSSOMoziaWalletHistory(ctx)
		var response struct {
			Success bool                     `json:"success"`
			Data    model.MoziaWalletHistory `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		return response.Success, response.Data, recorder.Body.String()
	}
	ok, page, body := read(2, "?limit=2&user_id=3")
	require.True(t, ok)
	require.Len(t, page.Items, 2)
	assert.Equal(t, 10, page.Items[0].Delta)
	assert.Equal(t, 90, page.Items[0].BalanceAfter)
	assert.Equal(t, "余额更正", page.Items[0].PublicNote)
	assert.Equal(t, -20, page.Items[1].Delta)
	assert.Equal(t, 80, page.Items[1].BalanceAfter)
	assert.Empty(t, page.Items[1].PublicNote)
	assert.Equal(t, page.Items[1].Id, page.NextCursor)
	for _, secret := range []string{"private", "other customer", "metadata", "reference_id", "user_id", "admin"} {
		assert.NotContains(t, body, secret)
	}
	// A concurrent new credit cannot shift the next page or repeat an entry.
	newCredit := adjustMoziaWalletForTest(t, `{"source":"gift","delta":5}`, 2)
	require.True(t, newCredit.Success)
	ok, older, _ := read(2, fmt.Sprintf("?limit=2&before=%d", page.NextCursor))
	require.True(t, ok)
	require.Len(t, older.Items, 1)
	assert.Equal(t, 100, older.Items[0].Delta)
	assert.Equal(t, "线下付款补登", older.Items[0].PublicNote)
	assert.Zero(t, older.NextCursor)

	for _, event := range []string{"topup", "redeem", "register_gift", "invite_gift", "refund"} {
		row := model.MoziaWalletTransaction{UserId: 2, Source: "gift", Delta: 1, EventType: event, Metadata: "legacy invalid JSON"}
		require.NoError(t, db.Create(&row).Error)
	}
	ok, all, _ := read(2, "")
	require.True(t, ok)
	assert.Len(t, all.Items, 9)
	assert.Empty(t, all.Items[0].PublicNote)
	ok, empty, _ := read(4, "")
	assert.True(t, ok)
	assert.Empty(t, empty.Items)
	assert.Zero(t, empty.NextCursor)
	ok, _, _ = read(0, "")
	assert.False(t, ok)
	for _, query := range []string{"?limit=0", "?limit=51", "?limit=-1", "?limit=1.5", "?before=-1", "?before=secret"} {
		ok, _, _ := read(2, query)
		assert.False(t, ok, query)
	}
}
