package controller

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedFundedToken(t *testing.T, db *gorm.DB, userID int, remain int) *model.Token {
	t.Helper()
	tk := &model.Token{
		UserId: userID, Name: "pack-h3", Key: "sk-funded-" + common.GetRandomString(8),
		Status: common.TokenStatusEnabled, CreatedTime: 1, AccessedTime: 1,
		ExpiredTime: 1_900_000_000, RemainQuota: remain, UnlimitedQuota: false,
		ModelLimitsEnabled: true, ModelLimits: "minimax/minimax-h3-t2va", Group: "auto", SelfFunded: true,
	}
	if err := db.Create(tk).Error; err != nil {
		t.Fatalf("seed funded token: %v", err)
	}
	return tk
}

// 复现验收 P1：matrix 普通用户改 key 走的是 SSO 桥（上下文带 sso_sub），
// 之前只要有 sso_sub 就放行，用户能把包内余额 500 改 50000、有效期改永久、关掉模型限制。
func TestUpdateToken_SSOContextCannotChangeFundedFields(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	tk := seedFundedToken(t, db, 1, 500)

	body := map[string]any{
		"id": tk.Id, "name": "renamed", "expired_time": -1, "remain_quota": 50000,
		"unlimited_quota": false, "model_limits_enabled": false, "model_limits": "",
		"group": "auto", "cross_group_retry": false,
	}
	ctx, rec := newAuthenticatedContext(t, http.MethodPut, "/api/sso/token/", body, 1)
	ctx.Set("sso_sub", "user-sso-sub")
	UpdateToken(ctx)
	if resp := decodeAPIResponse(t, rec); !resp.Success {
		t.Fatalf("改名应成功: %s", resp.Message)
	}

	var got model.Token
	if err := db.First(&got, tk.Id).Error; err != nil {
		t.Fatal(err)
	}
	if got.Name != "renamed" {
		t.Fatalf("改名未生效: %q", got.Name)
	}
	if got.RemainQuota != 500 || got.ExpiredTime != 1_900_000_000 || !got.ModelLimitsEnabled || got.ModelLimits != "minimax/minimax-h3-t2va" {
		t.Fatalf("权益字段被普通更新接口改动: remain=%d expired=%d limits=%v/%q", got.RemainQuota, got.ExpiredTime, got.ModelLimitsEnabled, got.ModelLimits)
	}
}

func TestUpdateToken_SSOContextCanToggleFundedStatus(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	tk := seedFundedToken(t, db, 1, 500)
	ctx, rec := newAuthenticatedContext(t, http.MethodPut, "/api/sso/token/?status_only=true", map[string]any{"id": tk.Id, "status": common.TokenStatusDisabled}, 1)
	ctx.Set("sso_sub", "user-sso-sub")
	UpdateToken(ctx)
	if resp := decodeAPIResponse(t, rec); !resp.Success {
		t.Fatalf("启停应成功: %s", resp.Message)
	}
	var got model.Token
	db.First(&got, tk.Id)
	if got.Status != common.TokenStatusDisabled {
		t.Fatalf("status 未改为停用: %d", got.Status)
	}
}

func TestDeleteToken_SSOContextCannotDeleteFunded(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	tk := seedFundedToken(t, db, 1, 500)

	ctx, rec := newAuthenticatedContext(t, http.MethodDelete, "/api/sso/token/1", nil, 1)
	ctx.Set("sso_sub", "user-sso-sub")
	ctx.Params = gin.Params{{Key: "id", Value: itoa(tk.Id)}}
	DeleteToken(ctx)
	if resp := decodeAPIResponse(t, rec); resp.Success {
		t.Fatal("经 SSO 桥删除权益包 key 应被拒绝")
	}
	ctx, rec = newAuthenticatedContext(t, http.MethodPost, "/api/sso/token/batch", map[string]any{"ids": []int{tk.Id}}, 1)
	ctx.Set("sso_sub", "user-sso-sub")
	DeleteTokenBatch(ctx)
	if resp := decodeAPIResponse(t, rec); resp.Success {
		t.Fatal("批量删除权益包 key 应被拒绝")
	}
	var cnt int64
	db.Model(&model.Token{}).Where("id = ?", tk.Id).Count(&cnt)
	if cnt != 1 {
		t.Fatal("权益包 key 被删除了")
	}
}

// 普通建 key 接口哪怕带 sso_sub 也不能自带资金（否则用户可自己发钱）
func TestAddToken_IgnoresSelfFundedEvenWithSSO(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	body := map[string]any{"name": "self-issued", "remain_quota": 999999, "unlimited_quota": false, "self_funded": true, "expired_time": -1, "group": "auto"}
	ctx, rec := newAuthenticatedContext(t, http.MethodPost, "/api/sso/token/", body, 1)
	ctx.Set("sso_sub", "user-sso-sub")
	AddToken(ctx)
	if resp := decodeAPIResponse(t, rec); !resp.Success {
		t.Fatalf("建 key 应成功: %s", resp.Message)
	}
	var got model.Token
	db.Where("name = ?", "self-issued").First(&got)
	if got.SelfFunded {
		t.Fatal("普通建 key 接口签出了自带资金的令牌")
	}
}

// 受独立密钥保护的管理接口：签发 / 调整 / 撤销
func TestFundedTokenRoutes_IssueAdjustRevoke(t *testing.T) {
	db := setupTokenControllerTestDB(t)

	ctx, rec := newAuthenticatedContext(t, http.MethodPost, "/api/sso/funded-token/", map[string]any{
		"name": "pack-h3", "remain_quota": 500, "expired_time": 1_900_000_000,
		"model_limits_enabled": true, "model_limits": "minimax/minimax-h3-t2va",
	}, 1)
	IssueSelfFundedToken(ctx)
	resp := decodeAPIResponse(t, rec)
	if !resp.Success {
		t.Fatalf("签发失败: %s", resp.Message)
	}
	var issued tokenResponseItem
	common.Unmarshal(resp.Data, &issued)
	var got model.Token
	db.First(&got, issued.ID)
	if !got.SelfFunded || got.RemainQuota != 500 || got.UnlimitedQuota || got.Group != "auto" {
		t.Fatalf("签发结果不对: %+v", got)
	}

	ctx, rec = newAuthenticatedContext(t, http.MethodPut, "/api/sso/funded-token/1", map[string]any{"remain_quota": 800, "status": common.TokenStatusDisabled}, 1)
	ctx.Params = gin.Params{{Key: "id", Value: itoa(got.Id)}}
	AdjustSelfFundedToken(ctx)
	if resp := decodeAPIResponse(t, rec); !resp.Success {
		t.Fatalf("调整失败: %s", resp.Message)
	}
	db.First(&got, issued.ID)
	if got.RemainQuota != 800 || got.Status != common.TokenStatusDisabled || got.ModelLimits != "minimax/minimax-h3-t2va" {
		t.Fatalf("调整结果不对: %+v", got)
	}

	// 对非资金令牌不可用
	plain := seedToken(t, db, 1, "plain", "plainkey1234567890")
	ctx, rec = newAuthenticatedContext(t, http.MethodDelete, "/api/sso/funded-token/x", nil, 1)
	ctx.Params = gin.Params{{Key: "id", Value: itoa(plain.Id)}}
	RevokeSelfFundedToken(ctx)
	if resp := decodeAPIResponse(t, rec); resp.Success {
		t.Fatal("普通 key 不应能走撤销接口")
	}

	ctx, rec = newAuthenticatedContext(t, http.MethodDelete, "/api/sso/funded-token/x", nil, 1)
	ctx.Params = gin.Params{{Key: "id", Value: itoa(got.Id)}}
	RevokeSelfFundedToken(ctx)
	if resp := decodeAPIResponse(t, rec); !resp.Success {
		t.Fatalf("撤销失败: %s", resp.Message)
	}
	var cnt int64
	db.Model(&model.Token{}).Where("id = ?", got.Id).Count(&cnt)
	if cnt != 0 {
		t.Fatal("撤销后令牌仍在")
	}
}

func TestRevokeSelfFundedToken_RetryPreservesRevocation(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	token := seedFundedToken(t, db, 1, 500)
	for attempt := 0; attempt < 2; attempt++ {
		ctx, rec := newAuthenticatedContext(t, http.MethodDelete, "/api/sso/funded-token/1", nil, 1)
		ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(token.Id)}}
		RevokeSelfFundedToken(ctx)
		require.True(t, decodeAPIResponse(t, rec).Success, "an uncertain revoke can be retried")
	}
	var revoked model.Token
	require.NoError(t, db.Unscoped().First(&revoked, token.Id).Error)
	assert.True(t, revoked.DeletedAt.Valid)
	_, err := model.ValidateUserToken(token.Key)
	require.Error(t, err, "a revoked key must not authorize consumption")

	ctx, rec := newAuthenticatedContext(t, http.MethodPut, "/api/sso/token/?status_only=true", map[string]any{
		"id": token.Id, "status": common.TokenStatusEnabled,
	}, 1)
	UpdateToken(ctx)
	assert.False(t, decodeAPIResponse(t, rec).Success, "ordinary enable must not restore revoked funds")
}

func TestRevokeSelfFundedToken_RejectsInvalidTargets(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	other := seedFundedToken(t, db, 2, 500)
	otherRevoked := seedFundedToken(t, db, 2, 500)
	require.NoError(t, db.Delete(otherRevoked).Error)
	plain := seedToken(t, db, 1, "plain", "plain-not-funded")
	plainRevoked := seedToken(t, db, 1, "plain revoked", "plain-revoked-not-funded")
	require.NoError(t, db.Delete(plainRevoked).Error)
	for _, tc := range []struct {
		name string
		id   int
	}{
		{"other owner", other.Id},
		{"other owner already revoked", otherRevoked.Id},
		{"ordinary key", plain.Id},
		{"ordinary key already deleted", plainRevoked.Id},
		{"unknown key", plainRevoked.Id + 1000},
		{"invalid id", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, rec := newAuthenticatedContext(t, http.MethodDelete, "/api/sso/funded-token/1", nil, 1)
			ctx.Params = gin.Params{{Key: "id", Value: strconv.Itoa(tc.id)}}
			RevokeSelfFundedToken(ctx)
			assert.False(t, decodeAPIResponse(t, rec).Success)
		})
	}
	var active int64
	require.NoError(t, db.Model(&model.Token{}).Count(&active).Error)
	assert.EqualValues(t, 2, active, "rejected requests must leave both active keys intact")
}

func TestSSOFundingAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	run := func(envKey, header string) int {
		t.Setenv("SSO_FUNDING_KEY", envKey)
		r := gin.New()
		r.GET("/p", middleware.SSOFundingAuth(), func(c *gin.Context) { c.Status(http.StatusOK) })
		req := httptest.NewRequest(http.MethodGet, "/p", nil)
		if header != "" {
			req.Header.Set(middleware.SSOFundingKeyHeader, header)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	if c := run("", "anything"); c != http.StatusServiceUnavailable {
		t.Fatalf("未配置密钥应 503, got %d", c)
	}
	if c := run("secret-1", ""); c != http.StatusForbidden {
		t.Fatalf("缺头应 403, got %d", c)
	}
	if c := run("secret-1", "wrong"); c != http.StatusForbidden {
		t.Fatalf("错密钥应 403, got %d", c)
	}
	if c := run("secret-1", "secret-1"); c != http.StatusOK {
		t.Fatalf("正确密钥应放行, got %d", c)
	}
}

func itoa(i int) string { return strconvItoa(i) }

func strconvItoa(i int) string { return strconv.Itoa(i) }

// 复现验收 P1（第二轮）：改名 / 启停 / 管理调整 读取 token 后、写回前，另一笔请求原子扣掉 300。
// 旧实现整行回写把余额恢复成 500；现在只写涉及的列，余额必须始终是 200、已用 300。
func TestTokenWrites_DoNotRestoreConsumedBalance(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	const remainAfter, usedAfter = 200, 300
	interleave := func(id int) func() {
		return func() {
			if err := model.DecreaseSelfFundedTokenQuota(id, "", 300); err != nil {
				t.Fatalf("interleaved decrease: %v", err)
			}
		}
	}
	check := func(id int, label string) {
		t.Helper()
		var got model.Token
		if err := db.First(&got, id).Error; err != nil {
			t.Fatal(err)
		}
		if got.RemainQuota != remainAfter || got.UsedQuota != usedAfter {
			t.Fatalf("%s 把已消费余额写回了: remain=%d used=%d", label, got.RemainQuota, got.UsedQuota)
		}
	}

	// 1) 普通改名（经 SSO 桥）
	tk := seedFundedToken(t, db, 1, 500)
	tokenUpdateTestHook = interleave(tk.Id)
	ctx, rec := newAuthenticatedContext(t, http.MethodPut, "/api/sso/token/", map[string]any{
		"id": tk.Id, "name": "renamed", "expired_time": -1, "remain_quota": 500, "unlimited_quota": false,
		"model_limits_enabled": true, "model_limits": "minimax/minimax-h3-t2va", "group": "auto",
	}, 1)
	ctx.Set("sso_sub", "user-sso-sub")
	UpdateToken(ctx)
	if resp := decodeAPIResponse(t, rec); !resp.Success {
		t.Fatalf("改名失败: %s", resp.Message)
	}
	check(tk.Id, "改名")
	var renamed model.Token
	db.First(&renamed, tk.Id)
	if renamed.Name != "renamed" {
		t.Fatal("改名未生效")
	}

	// 2) 普通停用（status_only）
	tk = seedFundedToken(t, db, 1, 500)
	tokenUpdateTestHook = interleave(tk.Id)
	ctx, rec = newAuthenticatedContext(t, http.MethodPut, "/api/sso/token/?status_only=true", map[string]any{"id": tk.Id, "status": common.TokenStatusDisabled}, 1)
	UpdateToken(ctx)
	if resp := decodeAPIResponse(t, rec); !resp.Success {
		t.Fatalf("停用失败: %s", resp.Message)
	}
	check(tk.Id, "停用")
	var disabled model.Token
	db.First(&disabled, tk.Id)
	if disabled.Status != common.TokenStatusDisabled {
		t.Fatal("停用未生效")
	}

	// 3) 管理接口只调整有效期
	tk = seedFundedToken(t, db, 1, 500)
	tokenUpdateTestHook = interleave(tk.Id)
	ctx, rec = newAuthenticatedContext(t, http.MethodPut, "/api/sso/funded-token/x", map[string]any{"expired_time": 1_950_000_000}, 1)
	ctx.Params = gin.Params{{Key: "id", Value: itoa(tk.Id)}}
	AdjustSelfFundedToken(ctx)
	if resp := decodeAPIResponse(t, rec); !resp.Success {
		t.Fatalf("调整失败: %s", resp.Message)
	}
	check(tk.Id, "管理调整有效期")
	var adjusted model.Token
	db.First(&adjusted, tk.Id)
	if adjusted.ExpiredTime != 1_950_000_000 {
		t.Fatal("有效期未生效")
	}
	tokenUpdateTestHook = nil

	// 响应体应反映数据库最新余额，而不是读取时的旧值
	var body struct {
		RemainQuota int `json:"remain_quota"`
	}
	resp := decodeAPIResponse(t, rec)
	if err := common.Unmarshal(resp.Data, &body); err != nil || body.RemainQuota != remainAfter {
		t.Fatalf("响应体余额应为 %d, got %d (err=%v)", remainAfter, body.RemainQuota, err)
	}
}

// 余额耗尽被标 Exhausted 的权益包 key，运营补额度后应自动回到启用；到期同理
func TestAdjustSelfFundedToken_RevivesAutoDisabledStatus(t *testing.T) {
	db := setupTokenControllerTestDB(t)
	tk := seedFundedToken(t, db, 1, 0)
	db.Model(&model.Token{}).Where("id = ?", tk.Id).Update("status", common.TokenStatusExhausted)
	ctx, rec := newAuthenticatedContext(t, http.MethodPut, "/api/sso/funded-token/x", map[string]any{"remain_quota": 1000}, 1)
	ctx.Params = gin.Params{{Key: "id", Value: itoa(tk.Id)}}
	AdjustSelfFundedToken(ctx)
	if resp := decodeAPIResponse(t, rec); !resp.Success {
		t.Fatalf("补额度失败: %s", resp.Message)
	}
	var got model.Token
	db.First(&got, tk.Id)
	if got.Status != common.TokenStatusEnabled || got.RemainQuota != 1000 {
		t.Fatalf("补额度后应自动启用: status=%d remain=%d", got.Status, got.RemainQuota)
	}

	db.Model(&model.Token{}).Where("id = ?", tk.Id).Update("status", common.TokenStatusExpired)
	ctx, rec = newAuthenticatedContext(t, http.MethodPut, "/api/sso/funded-token/x", map[string]any{"expired_time": 1_950_000_000}, 1)
	ctx.Params = gin.Params{{Key: "id", Value: itoa(tk.Id)}}
	AdjustSelfFundedToken(ctx)
	db.First(&got, tk.Id)
	if got.Status != common.TokenStatusEnabled {
		t.Fatalf("延期后应自动启用: status=%d", got.Status)
	}

	// 运营主动停用的（Disabled）不会被补额度顺手打开
	db.Model(&model.Token{}).Where("id = ?", tk.Id).Update("status", common.TokenStatusDisabled)
	ctx, _ = newAuthenticatedContext(t, http.MethodPut, "/api/sso/funded-token/x", map[string]any{"remain_quota": 2000}, 1)
	ctx.Params = gin.Params{{Key: "id", Value: itoa(tk.Id)}}
	AdjustSelfFundedToken(ctx)
	db.First(&got, tk.Id)
	if got.Status != common.TokenStatusDisabled {
		t.Fatal("人工停用不应被补额度恢复")
	}
}
