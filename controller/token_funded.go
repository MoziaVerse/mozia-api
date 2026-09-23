package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// 自带资金（self_funded）令牌的管理接口：签发 / 调整权益 / 撤销。
// 挂在 /api/sso/funded-token 下，除 SSOAuth 外还要过 SSOFundingAuth（独立密钥）。
// 普通的 /api/sso/token 接口对这类令牌只允许改名和启停（见 UpdateToken / selfFundedDeleteGuard）。

type fundedTokenIssueRequest struct {
	Name               string `json:"name"`
	RemainQuota        int    `json:"remain_quota"`
	ExpiredTime        int64  `json:"expired_time"`
	ModelLimitsEnabled bool   `json:"model_limits_enabled"`
	ModelLimits        string `json:"model_limits"`
	Group              string `json:"group"`
}

type fundedTokenAdjustRequest struct {
	RemainQuota        *int    `json:"remain_quota"`
	ExpiredTime        *int64  `json:"expired_time"`
	ModelLimitsEnabled *bool   `json:"model_limits_enabled"`
	ModelLimits        *string `json:"model_limits"`
	Status             *int    `json:"status"`
}

func validFundedQuota(c *gin.Context, quota int) bool {
	if quota <= 0 {
		common.ApiErrorMsg(c, "自带资金的令牌必须设置大于 0 的额度")
		return false
	}
	if quota > int(1000000000*common.QuotaPerUnit) {
		common.ApiErrorI18n(c, i18n.MsgTokenQuotaExceedMax, map[string]any{"Max": int(1000000000 * common.QuotaPerUnit)})
		return false
	}
	return true
}

// IssueSelfFundedToken 为当前 SSO 用户签发一把自带资金的令牌，返回创建结果（含 id，无需回查）。
func IssueSelfFundedToken(c *gin.Context) {
	var req fundedTokenIssueRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(req.Name) == 0 || len(req.Name) > 50 {
		common.ApiErrorI18n(c, i18n.MsgTokenNameTooLong)
		return
	}
	if !validFundedQuota(c, req.RemainQuota) {
		return
	}
	key, err := common.GenerateKey()
	if err != nil {
		common.ApiErrorI18n(c, i18n.MsgTokenGenerateFailed)
		return
	}
	group := req.Group
	if group == "" {
		group = "auto"
	}
	tk := model.Token{
		UserId:             c.GetInt("id"),
		Name:               req.Name,
		Key:                key,
		CreatedTime:        common.GetTimestamp(),
		AccessedTime:       common.GetTimestamp(),
		ExpiredTime:        req.ExpiredTime,
		RemainQuota:        req.RemainQuota,
		UnlimitedQuota:     false,
		ModelLimitsEnabled: req.ModelLimitsEnabled,
		ModelLimits:        req.ModelLimits,
		Group:              group,
		SelfFunded:         true,
	}
	if err := tk.Insert(); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": buildMaskedTokenResponse(&tk)})
}

func loadFundedToken(c *gin.Context) (*model.Token, bool) {
	id, _ := strconv.Atoi(c.Param("id"))
	tk, err := model.GetTokenByIds(id, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return nil, false
	}
	if !tk.SelfFunded {
		common.ApiErrorMsg(c, "该令牌不是自带资金的令牌")
		return nil, false
	}
	return tk, true
}

// AdjustSelfFundedToken 调整权益：额度 / 有效期 / 模型范围 / 启停，只改传入的字段。
func AdjustSelfFundedToken(c *gin.Context) {
	var req fundedTokenAdjustRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	tk, ok := loadFundedToken(c)
	if !ok {
		return
	}
	if tokenUpdateTestHook != nil {
		tokenUpdateTestHook()
	}
	// 只写明确提交的字段；没提交 remain_quota 就绝不碰余额，否则会覆盖并发扣减
	columns := []string{}
	if req.RemainQuota != nil {
		if !validFundedQuota(c, *req.RemainQuota) {
			return
		}
		tk.RemainQuota = *req.RemainQuota
		columns = append(columns, "remain_quota")
	}
	if req.ExpiredTime != nil {
		tk.ExpiredTime = *req.ExpiredTime
		columns = append(columns, "expired_time")
	}
	if req.ModelLimitsEnabled != nil {
		tk.ModelLimitsEnabled = *req.ModelLimitsEnabled
		columns = append(columns, "model_limits_enabled")
	}
	if req.ModelLimits != nil {
		tk.ModelLimits = *req.ModelLimits
		columns = append(columns, "model_limits")
	}
	if req.Status != nil {
		if *req.Status != common.TokenStatusEnabled && *req.Status != common.TokenStatusDisabled {
			common.ApiErrorMsg(c, "status 只能是启用或停用")
			return
		}
		tk.Status = *req.Status
		columns = append(columns, "status")
	}
	if len(columns) == 0 {
		common.ApiErrorMsg(c, "没有需要调整的字段")
		return
	}
	// 余额耗尽 / 到期后校验会把 status 写成 Exhausted / Expired；运营补额度或延期时若没显式
	// 传 status，令牌会一直停在失效态，等于补了也用不了。这两种自动失效态随调整自动恢复。
	if req.Status == nil {
		revived := (req.RemainQuota != nil && tk.Status == common.TokenStatusExhausted) ||
			(req.ExpiredTime != nil && tk.Status == common.TokenStatusExpired)
		if revived {
			tk.Status = common.TokenStatusEnabled
			columns = append(columns, "status")
		}
	}
	if err := tk.UpdateColumns(columns...); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": buildMaskedTokenResponse(tk)})
}

// RevokeSelfFundedToken 撤销自带资金的令牌；同一用户重试已撤销的令牌仍返回成功。
func RevokeSelfFundedToken(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if err := model.RevokeSelfFundedToken(id, c.GetInt("id")); err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": ""})
}
