package controller

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// GetVideoQueueStatus 供客户端展示"前方还有几个任务、预计等多久"。
// 挂在 /v1 下走 TokenAuth：按 token 所属用户返回本人在途任务与所在 worker 的位置。
// 只返回真实采集到的数字，拿不到就是 null，客户端只显示"处理中"。
func GetVideoQueueStatus(c *gin.Context) {
	userId := c.GetInt(string(constant.ContextKeyUserId))
	if userId == 0 {
		c.JSON(401, gin.H{"error": gin.H{"code": "unauthorized", "message": "未识别到用户"}})
		return
	}
	userGroup := common.GetContextKeyString(c, constant.ContextKeyUserGroup)
	tokenGroup := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
	view := service.BuildTaskQueueView(userId, userGroup, tokenGroup, c.Query("model"))
	c.JSON(200, view)
}

// ---- 管理端：/api/mozia/task-limit ----

// GetMoziaTaskLimitWorkers 各 worker 的队列画像（看板 / 假活列表）。
func GetMoziaTaskLimitWorkers(c *gin.Context) {
	common.ApiSuccess(c, service.ListWorkerQueueSnapshots())
}

type taskLimitOverrideView struct {
	model.MoziaUserTaskLimitOverride
	Active   bool   `json:"active"`
	Username string `json:"username,omitempty"`
}

func GetMoziaTaskLimitOverrides(c *gin.Context) {
	rows, err := model.ListTaskLimitOverrides()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	now := time.Now()
	out := make([]taskLimitOverrideView, 0, len(rows))
	for i := range rows {
		v := taskLimitOverrideView{MoziaUserTaskLimitOverride: rows[i], Active: rows[i].IsActive(now)}
		if u, err := model.GetUserById(rows[i].UserId, false); err == nil && u != nil {
			v.Username = u.Username
		}
		out = append(out, v)
	}
	common.ApiSuccess(c, out)
}

type upsertTaskLimitOverrideRequest struct {
	UserId     int     `json:"user_id"`
	Running    int     `json:"running"`
	Queued     int     `json:"queued"`
	PerMinute  int     `json:"per_minute"`
	DailyYuan  float64 `json:"daily_yuan"`
	FailStreak int     `json:"fail_streak"`
	// 二选一：until（unix 秒）或 duration_minutes；都不给 = 永久
	Until           int64  `json:"until"`
	DurationMinutes int    `json:"duration_minutes"`
	Reason          string `json:"reason"`
}

// UpsertMoziaTaskLimitOverride 写入 / 更新一条用户覆盖：
//
//	降速 → running/queued 给更小值 + duration；冷却 → 全 0 + duration；暂停 → 全 0 不给时长。
func UpsertMoziaTaskLimitOverride(c *gin.Context) {
	var req upsertTaskLimitOverrideRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if req.UserId <= 0 {
		common.ApiErrorMsg(c, "user_id 必填")
		return
	}
	if req.Running < 0 || req.Queued < 0 || req.PerMinute < 0 || req.DailyYuan < 0 || req.FailStreak < 0 {
		common.ApiErrorMsg(c, "限制值不能为负")
		return
	}
	if u, err := model.GetUserById(req.UserId, false); err != nil || u == nil {
		common.ApiErrorMsg(c, "用户不存在")
		return
	}
	until := req.Until
	if until == 0 && req.DurationMinutes > 0 {
		until = time.Now().Add(time.Duration(req.DurationMinutes) * time.Minute).Unix()
	}
	o := &model.MoziaUserTaskLimitOverride{
		UserId: req.UserId, Running: req.Running, Queued: req.Queued, PerMinute: req.PerMinute,
		DailyYuan: req.DailyYuan, FailStreak: req.FailStreak, Until: until,
		Reason:   strings.TrimSpace(req.Reason),
		Operator: c.GetString("username"),
	}
	if err := model.UpsertTaskLimitOverride(o); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, o)
}

func DeleteMoziaTaskLimitOverride(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("user_id"))
	if err != nil || userId <= 0 {
		common.ApiErrorMsg(c, "user_id 无效")
		return
	}
	if err := model.DeleteTaskLimitOverride(userId); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// GetMoziaTaskLimitUserUsage 管理端看某个用户当前的用量与生效限制（排查投诉用）。
func GetMoziaTaskLimitUserUsage(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("user_id"))
	if err != nil || userId <= 0 {
		common.ApiErrorMsg(c, "user_id 无效")
		return
	}
	u, err := model.GetUserById(userId, false)
	if err != nil || u == nil {
		common.ApiErrorMsg(c, "用户不存在")
		return
	}
	common.ApiSuccess(c, service.BuildTaskQueueView(userId, u.Group, "", c.Query("model")))
}

// GetMoziaTaskLimitSyncClusters 返回「同一分钟内有 ≥ min_users 个不同用户提交任务」的分钟桶，
// 附每个用户的 sso_sub，供 matrix 侧按邀请人聚类识别多号同步提交的团伙。
// 只看提交时间，不区分模型；多号团伙的特征是节奏而不是模型。
func GetMoziaTaskLimitSyncClusters(c *gin.Context) {
	sinceMinutes, _ := strconv.Atoi(c.DefaultQuery("since_minutes", "60"))
	if sinceMinutes <= 0 || sinceMinutes > 24*60 {
		sinceMinutes = 60
	}
	minUsers, _ := strconv.Atoi(c.DefaultQuery("min_users", "4"))
	if minUsers < 2 {
		minUsers = 2
	}
	since := time.Now().Add(-time.Duration(sinceMinutes) * time.Minute).Unix()
	var rows []struct {
		Minute int64 `gorm:"column:minute"`
		UserId int   `gorm:"column:user_id"`
	}
	err := model.DB.Model(&model.Task{}).
		Select("(submit_time / 60) AS minute, user_id").
		Where("submit_time >= ?", since).
		Group("minute, user_id").
		Order("minute").
		Scan(&rows).Error
	if err != nil {
		common.ApiError(c, err)
		return
	}
	byMinute := map[int64][]int{}
	for _, r := range rows {
		byMinute[r.Minute] = append(byMinute[r.Minute], r.UserId)
	}
	userSet := map[int]struct{}{}
	minutes := make([]int64, 0, len(byMinute))
	for m, users := range byMinute {
		if len(users) < minUsers {
			continue
		}
		minutes = append(minutes, m)
		for _, u := range users {
			userSet[u] = struct{}{}
		}
	}
	sort.Slice(minutes, func(i, j int) bool { return minutes[i] < minutes[j] })
	ssoByUser := map[int]string{}
	if len(userSet) > 0 {
		ids := make([]int, 0, len(userSet))
		for u := range userSet {
			ids = append(ids, u)
		}
		var ssos []model.UserSSO
		if err := model.DB.Where("user_id IN ?", ids).Find(&ssos).Error; err == nil {
			for _, s := range ssos {
				ssoByUser[s.UserId] = s.SSOSub
			}
		}
	}
	type clusterUser struct {
		UserId int    `json:"user_id"`
		SSOSub string `json:"sso_sub"`
	}
	type cluster struct {
		MinuteStart int64         `json:"minute_start"`
		Users       []clusterUser `json:"users"`
	}
	out := make([]cluster, 0, len(minutes))
	for _, m := range minutes {
		users := byMinute[m]
		sort.Ints(users)
		cu := make([]clusterUser, 0, len(users))
		for _, u := range users {
			cu = append(cu, clusterUser{UserId: u, SSOSub: ssoByUser[u]})
		}
		out = append(out, cluster{MinuteStart: m * 60, Users: cu})
	}
	common.ApiSuccess(c, gin.H{"since": since, "min_users": minUsers, "clusters": out})
}
