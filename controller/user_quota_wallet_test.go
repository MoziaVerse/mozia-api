package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestManageUserQuotaKeepsWalletAndAuditConsistent(t *testing.T) {
	for _, tc := range []struct {
		name       string
		mode       string
		value      int
		staleQuota bool
		failLedger bool
		want       [3]int // gift, paid, legacy
		wantError  bool
	}{
		{name: "add unclassified credit", mode: "add", value: 30, want: [3]int{100, 50, 50}},
		{name: "subtract across sources", mode: "subtract", value: 120, want: [3]int{0, 30, 20}},
		{name: "override higher", mode: "override", value: 200, want: [3]int{100, 50, 50}},
		{name: "override lower", mode: "override", value: 20, want: [3]int{0, 0, 20}},
		{name: "override zero", mode: "override", value: 0},
		{name: "repair old aggregate-only zero", mode: "override", value: 0, staleQuota: true},
		{name: "insufficient debit", mode: "subtract", value: 171, want: [3]int{100, 50, 20}, wantError: true},
		{name: "negative override", mode: "override", value: -1, want: [3]int{100, 50, 20}, wantError: true},
		{name: "overflow addition", mode: "add", value: int(^uint(0) >> 1), want: [3]int{100, 50, 20}, wantError: true},
		{name: "zero addition", mode: "add", value: 0, want: [3]int{100, 50, 20}, wantError: true},
		{name: "invalid mode", mode: "unknown", value: 10, want: [3]int{100, 50, 20}, wantError: true},
		{name: "ledger write failure rolls back earlier source", mode: "override", value: 0, failLedger: true, want: [3]int{100, 50, 20}, wantError: true},
		{name: "ledger write failure rolls back mirror repair", mode: "override", value: 0, staleQuota: true, failLedger: true, want: [3]int{100, 50, 20}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupMoziaWalletAuditControllerTest(t)
			require.NoError(t, db.Create(&model.User{Id: 1, Username: "root", Role: common.RoleRootUser, AffCode: "root"}).Error)
			require.NoError(t, db.Create(&model.User{Id: 2, Username: "quota-user", Role: common.RoleCommonUser, AffCode: "quota-user"}).Error)
			sources := []string{model.MoziaWalletSourceGift, model.MoziaWalletSourcePaid, model.MoziaWalletSourceLegacy}
			initial := [3]int{100, 50, 20}
			for i, source := range sources {
				require.NoError(t, model.GrantMoziaWalletQuota(model.MoziaWalletGrantInput{
					UserId: 2, Source: source, Amount: initial[i], EventType: model.MoziaWalletEventTopUp,
				}))
			}
			if tc.staleQuota {
				require.NoError(t, db.Model(&model.User{}).Where("id = ?", 2).Update("quota", 0).Error)
			}
			if tc.failLedger {
				require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail_paid_ledger", func(tx *gorm.DB) {
					if row, ok := tx.Statement.Dest.(*model.MoziaWalletTransaction); ok && row.Source == model.MoziaWalletSourcePaid {
						tx.AddError(errors.New("ledger unavailable"))
					}
				}))
			}

			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodPost, "/api/user/manage", strings.NewReader(
				fmt.Sprintf(`{"id":2,"action":"add_quota","mode":%q,"value":%d}`, tc.mode, tc.value)))
			ctx.Request.Header.Set("Content-Type", "application/json")
			ctx.Set("id", 1)
			ctx.Set("username", "root")
			ctx.Set("role", common.RoleRootUser)
			ManageUser(ctx)
			var response struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.Equal(t, !tc.wantError, response.Success, response.Message)

			var user model.User
			require.NoError(t, db.First(&user, 2).Error)
			total := tc.want[0] + tc.want[1] + tc.want[2]
			wantQuota := total
			if tc.staleQuota && tc.wantError {
				wantQuota = 0
			}
			assert.Equal(t, wantQuota, user.Quota)
			var balances []model.MoziaWalletBalance
			require.NoError(t, db.Where("user_id = ?", 2).Find(&balances).Error)
			require.Len(t, balances, 3)
			for i, source := range sources {
				for _, balance := range balances {
					if balance.Source == source {
						assert.Equal(t, tc.want[i], balance.Balance, source)
					}
				}
			}
			var ledger []model.MoziaWalletTransaction
			require.NoError(t, db.Where("user_id = ? AND event_type = ?", 2, model.MoziaWalletEventAdjust).Find(&ledger).Error)
			var logs []model.Log
			require.NoError(t, db.Where("type = ?", model.LogTypeManage).Find(&logs).Error)
			if tc.wantError {
				assert.Empty(t, ledger)
				assert.Empty(t, logs)
				return
			}
			changes := make(map[string]int)
			for i, source := range sources {
				if delta := tc.want[i] - initial[i]; delta != 0 {
					changes[source] = delta
				}
			}
			require.Len(t, ledger, len(changes))
			for _, row := range ledger {
				assert.Equal(t, changes[row.Source], row.Delta)
				assert.Equal(t, "admin_adjust", row.ReferenceType)
				assert.Equal(t, "user.quota_"+tc.mode, row.ReferenceId)
			}
			require.Len(t, logs, 1)
			var audit moziaWalletAuditOther
			require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &audit))
			assert.Equal(t, 1, audit.AdminInfo.AdminID)
			assert.Equal(t, 2, audit.Op.Params.TargetUserID)
			assert.Equal(t, "user.quota_"+tc.mode, audit.Op.Action)
			if tc.mode == "override" {
				assert.Contains(t, logs[0].Content, logger.LogQuota(170))
				assert.Contains(t, logs[0].Content, logger.LogQuota(total))
			}
			history, err := model.GetMoziaWalletHistory(2, 0, 50)
			require.NoError(t, err)
			assert.Len(t, history.Items, 3+len(changes))
		})
	}
}
