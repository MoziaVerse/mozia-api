package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

type moziaQuotaPolicyRequest struct {
	ModelPattern   string `json:"model_pattern"`
	MatchType      string `json:"match_type"`
	AllowedSources string `json:"allowed_sources"`
	ConsumeOrder   string `json:"consume_order"`
	Enabled        *bool  `json:"enabled"`
	Priority       int    `json:"priority"`
}

type moziaWalletAdjustRequest struct {
	Source        string `json:"source"`
	Delta         *int   `json:"delta"`
	TargetBalance *int   `json:"target_balance"`
	Reason        string `json:"reason"`
}

func (req moziaQuotaPolicyRequest) toModel(id int) model.MoziaModelQuotaPolicy {
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return model.MoziaModelQuotaPolicy{
		Id:             id,
		ModelPattern:   strings.TrimSpace(req.ModelPattern),
		MatchType:      strings.TrimSpace(req.MatchType),
		AllowedSources: strings.TrimSpace(req.AllowedSources),
		ConsumeOrder:   strings.TrimSpace(req.ConsumeOrder),
		Enabled:        enabled,
		Priority:       req.Priority,
	}
}

func GetSSOMoziaWallet(c *gin.Context) {
	userId := c.GetInt("id")
	if userId == 0 {
		common.ApiErrorMsg(c, "SSO 用户未解析")
		return
	}
	wallet, err := model.GetMoziaWalletView(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, wallet)
}

func resolveMoziaWalletUserId(identifier string) (int, error) {
	identifier = strings.TrimSpace(identifier)
	userId, err := strconv.Atoi(identifier)
	if err != nil || userId <= 0 {
		user, lookupErr := model.GetUserByUsername(identifier)
		if lookupErr != nil {
			return 0, lookupErr
		}
		return user.Id, nil
	} else if _, lookupErr := model.GetUserById(userId, false); lookupErr != nil {
		return 0, lookupErr
	}
	return userId, nil
}

func GetMoziaUserWallet(c *gin.Context) {
	userId, err := resolveMoziaWalletUserId(c.Param("id"))
	if err != nil {
		common.ApiErrorMsg(c, "用户不存在")
		return
	}
	wallet, err := model.GetMoziaWalletView(userId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, wallet)
}

func AdjustMoziaUserWallet(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiErrorMsg(c, "无效的用户 ID")
		return
	}
	var req moziaWalletAdjustRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.Delta == nil && req.TargetBalance == nil {
		common.ApiErrorMsg(c, "delta 或 target_balance 必填")
		return
	}
	if req.Delta != nil && req.TargetBalance != nil {
		common.ApiErrorMsg(c, "delta 和 target_balance 只能填写一个")
		return
	}
	wallet, err := model.AdjustMoziaWalletBalance(model.MoziaWalletAdjustInput{
		UserId:        userId,
		Source:        req.Source,
		Delta:         req.Delta,
		TargetBalance: req.TargetBalance,
		Reason:        strings.TrimSpace(req.Reason),
	})
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, wallet)
}

func GetAllMoziaQuotaPolicies(c *gin.Context) {
	policies, err := model.GetAllMoziaModelQuotaPolicies()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, policies)
}

func CreateMoziaQuotaPolicy(c *gin.Context) {
	var req moziaQuotaPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	policy := req.toModel(0)
	if err := model.CreateMoziaModelQuotaPolicy(&policy); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, &policy)
}

func UpdateMoziaQuotaPolicy(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "无效的策略 ID")
		return
	}
	var req moziaQuotaPolicyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	policy := req.toModel(id)
	if err := model.UpdateMoziaModelQuotaPolicy(&policy); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, &policy)
}

func DeleteMoziaQuotaPolicy(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "无效的策略 ID")
		return
	}
	if err := model.DeleteMoziaModelQuotaPolicy(id); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}
