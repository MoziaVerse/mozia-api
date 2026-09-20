package service

// 异步任务（视频类）的提交前限制：运行并发、排队上限、分钟提交、日消费、连续失败。
//
// 规则来源是运营《套餐与排队运营规则》：所有限制在创建上游任务和扣费前执行，
// 被拦的请求不产生上游成本；按用户计数，多建 key 不能放大；返回中文原因、
// 当前上限、当前运行 / 排队数和 Retry-After。
//
// 生效顺序：用户级覆盖（冷却 / 暂停 / 降速）> 分组配置。覆盖里 0 表示禁止，
// 分组配置里 0 表示不限。TaskLimitEnforce=false 时只记录不拦截（观察模式）。

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
)

const (
	TaskLimitErrorCode = "task_limit_exceeded"
	// 超过这个时长仍未终结的任务视为僵尸，不占名额（H3 p90 89 分钟，max 142 分钟）。
	taskLimitLeaseSeconds = 3 * 3600
	// 连续失败冷却时长
	taskLimitFailCooldown = 10 * time.Minute
	taskLimitRecentLimit  = 300
)

// TaskLimitDecision 描述一次拒绝（或观察模式下的"本应拒绝"）。
type TaskLimitDecision struct {
	Kind       string `json:"kind"` // running | queued | per_minute | daily_yuan | fail_streak | suspended
	Message    string `json:"message"`
	Limit      any    `json:"limit"`
	Current    any    `json:"current"`
	Running    int    `json:"running"`
	Queued     int    `json:"queued"`
	Group      string `json:"group"`
	Override   bool   `json:"override"`
	RetryAfter int    `json:"retry_after"` // 秒
	Observed   bool   `json:"observed"`    // 观察模式：未真正拦截
}

// TaskLimitUsage 是用户在限制范围内的当前用量，队列接口也复用它。
type TaskLimitUsage struct {
	Running       int
	Queued        int
	LastMinute    int
	DailyQuota    int64
	FailStreak    int
	LastFailAt    int64
	ActiveTasks   []*model.Task // 未终结且在租约内的任务，按 id 倒序
	EffectiveConf setting.TaskLimitConfig
	ConfFound     bool
	Override      *model.MoziaUserTaskLimitOverride
}

var taskLimitNow = time.Now

// ResolveUserGroup 取用户真实分组（token 的 auto 不算分组）。
func ResolveUserGroup(userGroup, tokenGroup string) string {
	g := strings.TrimSpace(tokenGroup)
	if g == "" || g == "auto" {
		g = strings.TrimSpace(userGroup)
	}
	return g
}

// CollectTaskLimitUsage 汇总用户在限制范围内的用量与生效配置。
func CollectTaskLimitUsage(userId int, group string, scopeMatch func(string) bool) *TaskLimitUsage {
	now := taskLimitNow()
	usage := &TaskLimitUsage{}
	if conf, ok := setting.GetGroupTaskLimit(group); ok {
		usage.EffectiveConf = conf
		usage.ConfFound = true
	}
	if o, err := model.GetActiveTaskLimitOverride(userId, now); err == nil && o != nil {
		// 覆盖只改写给了值（>0）的字段，其余沿用分组配置；running 与 queued 同为 0 表示暂停 / 冷却。
		usage.Override = o
		usage.ConfFound = true
		if o.Running > 0 {
			usage.EffectiveConf.Running = o.Running
		}
		if o.Queued > 0 {
			usage.EffectiveConf.Queued = o.Queued
		}
		if o.PerMinute > 0 {
			usage.EffectiveConf.PerMinute = o.PerMinute
		}
		if o.DailyYuan > 0 {
			usage.EffectiveConf.DailyYuan = o.DailyYuan
		}
		if o.FailStreak > 0 {
			usage.EffectiveConf.FailStreak = o.FailStreak
		}
	}

	nowUnix := now.Unix()
	tasks := model.GetUserRecentTasks(userId, nowUnix-86400, taskLimitRecentLimit)
	streakOpen := true
	for _, t := range tasks {
		if scopeMatch != nil && !scopeMatch(t.PublicModelName()) {
			continue
		}
		terminal := t.Status == model.TaskStatusSuccess || t.Status == model.TaskStatusFailure
		if !terminal && t.SubmitTime >= nowUnix-taskLimitLeaseSeconds {
			usage.ActiveTasks = append(usage.ActiveTasks, t)
			if t.Status == model.TaskStatusInProgress {
				usage.Running++
			} else {
				usage.Queued++
			}
		}
		if t.SubmitTime >= nowUnix-60 {
			usage.LastMinute++
		}
		if t.SubmitTime >= nowUnix-86400 && t.Status != model.TaskStatusFailure {
			usage.DailyQuota += int64(t.Quota)
		}
		// 连续失败：从最近一条往前数，遇到非失败的终态即止；未终结的跳过
		if streakOpen && terminal {
			if t.Status == model.TaskStatusFailure {
				usage.FailStreak++
				if usage.LastFailAt == 0 {
					usage.LastFailAt = t.FinishTime
					if usage.LastFailAt == 0 {
						usage.LastFailAt = t.SubmitTime
					}
				}
			} else {
				streakOpen = false
			}
		}
	}
	return usage
}

// CheckTaskSubmitLimit 在提交前判定。返回 nil 表示放行；返回 TaskError 时调用方直接响应。
// 观察模式下也返回 nil，但会记录一条 WARN 供上线前核对名单。
func CheckTaskSubmitLimit(ctx context.Context, userId int, userGroup, tokenGroup, modelName string) *dto.TaskError {
	if !setting.TaskLimitScopeMatches(modelName) {
		return nil
	}
	group := ResolveUserGroup(userGroup, tokenGroup)
	usage := CollectTaskLimitUsage(userId, group, setting.TaskLimitScopeMatches)
	decision := evaluateTaskLimit(usage, group, taskLimitNow())
	if decision == nil {
		return nil
	}
	if !setting.TaskLimitEnforce {
		decision.Observed = true
		logger.LogWarn(ctx, fmt.Sprintf("task limit observe: user=%d group=%s model=%s kind=%s limit=%v current=%v running=%d queued=%d override=%v",
			userId, group, modelName, decision.Kind, decision.Limit, decision.Current, decision.Running, decision.Queued, decision.Override))
		return nil
	}
	logger.LogInfo(ctx, fmt.Sprintf("task limit reject: user=%d group=%s model=%s kind=%s limit=%v current=%v",
		userId, group, modelName, decision.Kind, decision.Limit, decision.Current))
	return TaskLimitError(decision)
}

// TaskLimitError 把决策包装成 429 的 TaskError，Data 里带结构化字段。
func TaskLimitError(d *TaskLimitDecision) *dto.TaskError {
	return &dto.TaskError{
		Code:       TaskLimitErrorCode,
		Message:    d.Message,
		Type:       "task_limit",
		Data:       d,
		StatusCode: http.StatusTooManyRequests,
		LocalError: true,
		Error:      fmt.Errorf("%s", d.Message),
	}
}

func evaluateTaskLimit(usage *TaskLimitUsage, group string, now time.Time) *TaskLimitDecision {
	if !usage.ConfFound {
		return nil
	}
	conf := usage.EffectiveConf
	isOverride := usage.Override != nil
	base := TaskLimitDecision{Group: group, Override: isOverride, Running: usage.Running, Queued: usage.Queued}

	// 暂停 / 冷却：覆盖里 running 与 queued 同为 0
	if isOverride && usage.Override.Running == 0 && usage.Override.Queued == 0 {
		d := base
		d.Kind = "suspended"
		d.Limit, d.Current = 0, usage.Running+usage.Queued
		d.RetryAfter = 60
		if usage.Override.Until > 0 {
			if left := int(usage.Override.Until - now.Unix()); left > 0 {
				d.RetryAfter = left
			}
			d.Message = fmt.Sprintf("该账号处于冷却期，%d 分钟后可再次提交", (d.RetryAfter+59)/60)
		} else {
			d.Message = "该账号的视频生成已被暂停，请联系客服处理"
		}
		if r := strings.TrimSpace(usage.Override.Reason); r != "" {
			d.Message += "（" + r + "）"
		}
		return &d
	}

	// 运行并发与排队上限：提交时网关只能决定"要不要再放一条进 worker 队列"，
	// 新任务一定先进队列，所以排队满即拒绝；运行数由 worker 决定，网关只在
	// 总在途数超过 运行+排队 时兜底（worker 把超额任务拉起来跑的情况）。
	runningFull := conf.Running > 0 && usage.Running >= conf.Running
	queuedFull := conf.Queued > 0 && usage.Queued >= conf.Queued
	totalFull := conf.Running > 0 && conf.Queued > 0 && usage.Running+usage.Queued >= conf.Running+conf.Queued
	if queuedFull || totalFull {
		d := base
		d.Kind, d.Limit, d.Current = "queued", conf.Queued, usage.Queued
		d.RetryAfter = 60
		d.Message = fmt.Sprintf("排队中的视频任务已达上限（%d 个），当前运行 %d 个、排队 %d 个，请等待前面的任务完成", conf.Queued, usage.Running, usage.Queued)
		if runningFull {
			d.Message = fmt.Sprintf("同时运行 %d 个、排队 %d 个视频任务已达套餐上限（运行 %d / 排队 %d），请等待完成后再提交", usage.Running, usage.Queued, conf.Running, conf.Queued)
		}
		return &d
	}
	if conf.PerMinute > 0 && usage.LastMinute >= conf.PerMinute {
		d := base
		d.Kind, d.Limit, d.Current = "per_minute", conf.PerMinute, usage.LastMinute
		d.RetryAfter = 60
		d.Message = fmt.Sprintf("提交过于频繁（每分钟最多 %d 次），请稍后再试", conf.PerMinute)
		return &d
	}
	if conf.DailyYuan > 0 {
		spent := float64(usage.DailyQuota) / common.QuotaPerUnit
		if spent >= conf.DailyYuan {
			d := base
			d.Kind, d.Limit, d.Current = "daily_yuan", conf.DailyYuan, spent
			d.RetryAfter = 3600
			d.Message = fmt.Sprintf("24 小时内视频生成消费已达上限（¥%.0f），请明天再试或升级套餐", conf.DailyYuan)
			return &d
		}
	}
	if conf.FailStreak > 0 && usage.FailStreak >= conf.FailStreak && usage.LastFailAt > 0 {
		cooldownEnd := time.Unix(usage.LastFailAt, 0).Add(taskLimitFailCooldown)
		if now.Before(cooldownEnd) {
			d := base
			d.Kind, d.Limit, d.Current = "fail_streak", conf.FailStreak, usage.FailStreak
			d.RetryAfter = int(cooldownEnd.Sub(now).Seconds()) + 1
			d.Message = fmt.Sprintf("连续 %d 次任务失败，已进入冷却，%d 分钟后可再试；请先检查素材与参数", usage.FailStreak, (d.RetryAfter+59)/60)
			return &d
		}
	}
	return nil
}

// ---- 队列可见 ----

// TaskQueueItem 是用户一条在途任务在其所在 worker 上的位置。
type TaskQueueItem struct {
	TaskID     string `json:"task_id"`
	Model      string `json:"model"`
	Status     string `json:"status"`
	ChannelId  int    `json:"channel_id"`
	SubmitTime int64  `json:"submit_time"`
	StartTime  int64  `json:"start_time,omitempty"`
	// 同一 worker 上排在前面的任务数；拿不到 worker 画像时为 null
	Ahead *int `json:"ahead"`
	// 预计等待秒数 = ahead × 该 worker 近期 p50；样本不足为 null
	EtaSeconds *int `json:"eta_seconds"`
	// worker 是否被判假活
	WorkerSuspect bool `json:"worker_suspect"`
}

type TaskQueueView struct {
	Group     string                   `json:"group"`
	Enforce   bool                     `json:"enforce"`
	Limits    *setting.TaskLimitConfig `json:"limits"`
	Override  bool                     `json:"override"`
	Running   int                      `json:"running"`
	Queued    int                      `json:"queued"`
	Tasks     []TaskQueueItem          `json:"tasks"`
	Pool      TaskQueuePool            `json:"pool"`
	Blocked   *TaskLimitDecision       `json:"blocked"`
	SampledAt int64                    `json:"sampled_at"`
}

type TaskQueuePool struct {
	Workers       int     `json:"workers"`
	Running       int     `json:"running"`
	Queued        int     `json:"queued"`
	P50RunSeconds float64 `json:"p50_run_seconds"`
	HasTimeline   bool    `json:"has_timeline"`
}

// BuildTaskQueueView 组装队列接口响应；modelPrefix 为空时按 TaskLimitScope 过滤。
func BuildTaskQueueView(userId int, userGroup, tokenGroup, modelPrefix string) *TaskQueueView {
	group := ResolveUserGroup(userGroup, tokenGroup)
	match := setting.TaskLimitScopeMatches
	if p := strings.ToLower(strings.TrimSpace(modelPrefix)); p != "" {
		match = func(m string) bool { return strings.HasPrefix(strings.ToLower(m), p) }
	}
	usage := CollectTaskLimitUsage(userId, group, match)
	view := &TaskQueueView{
		Group:    group,
		Enforce:  setting.TaskLimitEnforce,
		Override: usage.Override != nil,
		Running:  usage.Running,
		Queued:   usage.Queued,
		Tasks:    []TaskQueueItem{},
	}
	if usage.ConfFound {
		conf := usage.EffectiveConf
		view.Limits = &conf
	}
	if d := evaluateTaskLimit(usage, group, taskLimitNow()); d != nil {
		view.Blocked = d
	}

	// 池子汇总 + 每条任务的位置
	snapshots := map[int]*WorkerQueueSnapshot{}
	var p50s []float64
	for _, s := range ListWorkerQueueSnapshots() {
		snapshots[s.ChannelId] = s
		view.Pool.Workers++
		view.Pool.Running += s.Running
		view.Pool.Queued += s.Queued
		if s.HasTimeline {
			view.Pool.HasTimeline = true
		}
		if s.P50RunSeconds > 0 {
			p50s = append(p50s, s.P50RunSeconds)
		}
		if s.SampledAt > view.SampledAt {
			view.SampledAt = s.SampledAt
		}
	}
	if len(p50s) > 0 {
		sum := 0.0
		for _, v := range p50s {
			sum += v
		}
		view.Pool.P50RunSeconds = sum / float64(len(p50s))
	}
	active := make([]*model.Task, len(usage.ActiveTasks))
	copy(active, usage.ActiveTasks)
	sort.Slice(active, func(i, j int) bool { // 按提交先后
		if active[i].SubmitTime != active[j].SubmitTime {
			return active[i].SubmitTime < active[j].SubmitTime
		}
		return active[i].ID < active[j].ID
	})
	for _, t := range active {
		item := TaskQueueItem{
			TaskID: t.TaskID, Model: t.PublicModelName(), Status: string(t.Status),
			ChannelId: t.ChannelId, SubmitTime: t.SubmitTime, StartTime: t.StartTime,
		}
		snap := snapshots[t.ChannelId]
		if snap == nil {
			snap = GetWorkerQueueSnapshot(t.ChannelId)
		}
		if snap != nil {
			item.WorkerSuspect = snap.Suspect
			if t.Status == model.TaskStatusInProgress {
				zero := 0
				item.Ahead = &zero
			} else if e, ok := snap.Active[t.GetUpstreamTaskID()]; ok && snap.HasTimeline {
				ahead := 0
				mine := e.QueuedAt
				if mine == 0 {
					mine = e.AcceptedAt
				}
				for id, other := range snap.Active {
					if id == e.ID || other.State != "queued" {
						continue
					}
					oq := other.QueuedAt
					if oq == 0 {
						oq = other.AcceptedAt
					}
					if oq < mine || (oq == mine && id < e.ID) {
						ahead++
					}
				}
				item.Ahead = &ahead
				if snap.P50RunSeconds > 0 {
					// 前面的任务串行跑完 + 当前 running 的剩余按半个 p50 估
					eta := int(float64(ahead)*snap.P50RunSeconds + snap.P50RunSeconds*0.5*float64(minInt(snap.Running, 1)))
					item.EtaSeconds = &eta
				}
			}
		}
		view.Tasks = append(view.Tasks, item)
	}
	return view
}
