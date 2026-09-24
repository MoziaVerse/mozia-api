package setting

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// TaskLimitConfig 是一个用户分组在异步任务（视频类）上的并发与频率约束。
// 0 表示该项不限制；对用户级覆盖表而言 0 表示禁止（见 service.CheckTaskSubmitLimit）。
type TaskLimitConfig struct {
	// 同时运行中的任务数上限
	Running int `json:"running"`
	// 排队中（已提交未开始）的任务数上限
	Queued int `json:"queued"`
	// 每分钟提交次数上限
	PerMinute int `json:"per_minute"`
	// 24 小时内消费上限（元）
	DailyYuan float64 `json:"daily_yuan"`
	// 连续失败达到该次数进入冷却
	FailStreak int `json:"fail_streak"`
}

var (
	taskLimitMu sync.RWMutex
	// GroupTaskLimits 按用户分组配置；未配置的分组不受限。
	GroupTaskLimits = map[string]TaskLimitConfig{}
	// TaskLimitScope 限制生效的模型前缀。空 = 不对任何模型生效（安全默认：只配了
	// GroupTaskLimits 忘了配范围，不会把限制误套到 Suno / MJ 等全部异步任务上）。
	TaskLimitScope = []string{}
	// TaskLimitEnforce 为 false 时只记录本应拦截的请求（观察模式），不真正拒绝。
	TaskLimitEnforce = false
)

func GroupTaskLimits2JSONString() string {
	taskLimitMu.RLock()
	defer taskLimitMu.RUnlock()
	b, err := json.Marshal(GroupTaskLimits)
	if err != nil {
		common.SysLog("error marshalling GroupTaskLimits: " + err.Error())
	}
	return string(b)
}

func UpdateGroupTaskLimitsByJSONString(jsonStr string) error {
	next := map[string]TaskLimitConfig{}
	if strings.TrimSpace(jsonStr) != "" {
		if err := json.Unmarshal([]byte(jsonStr), &next); err != nil {
			return err
		}
	}
	for g, c := range next {
		if c.Running < 0 || c.Queued < 0 || c.PerMinute < 0 || c.DailyYuan < 0 || c.FailStreak < 0 {
			return fmt.Errorf("group %s has negative task limit values", g)
		}
	}
	taskLimitMu.Lock()
	GroupTaskLimits = next
	taskLimitMu.Unlock()
	return nil
}

func TaskLimitScope2JSONString() string {
	taskLimitMu.RLock()
	defer taskLimitMu.RUnlock()
	b, err := json.Marshal(TaskLimitScope)
	if err != nil {
		common.SysLog("error marshalling TaskLimitScope: " + err.Error())
	}
	return string(b)
}

func UpdateTaskLimitScopeByJSONString(jsonStr string) error {
	next := []string{}
	if strings.TrimSpace(jsonStr) != "" {
		if err := json.Unmarshal([]byte(jsonStr), &next); err != nil {
			return err
		}
	}
	cleaned := make([]string, 0, len(next))
	for _, p := range next {
		if p = strings.TrimSpace(p); p != "" {
			cleaned = append(cleaned, p)
		}
	}
	taskLimitMu.Lock()
	TaskLimitScope = cleaned
	taskLimitMu.Unlock()
	return nil
}

// GetGroupTaskLimit 取分组配置；未配置返回 found=false。
func GetGroupTaskLimit(group string) (TaskLimitConfig, bool) {
	taskLimitMu.RLock()
	defer taskLimitMu.RUnlock()
	c, ok := GroupTaskLimits[group]
	return c, ok
}

// GetGroupTaskLimitForModel 取「分组 × 模型」的生效配置。
// 键 `<group>@<模型前缀>` 是该分组在某类模型上的独立名额（例如 `sub_basic@minimax/minimax-h3`），
// 命中最长前缀者优先；没有模型级键时回退到分组键，此时名额由范围内所有模型共用。
// 返回的 prefix 非空表示名额按该前缀单独计数。
func GetGroupTaskLimitForModel(group, modelName string) (conf TaskLimitConfig, prefix string, found bool) {
	taskLimitMu.RLock()
	defer taskLimitMu.RUnlock()
	m := strings.ToLower(strings.TrimSpace(modelName))
	best := ""
	for key, c := range GroupTaskLimits {
		g, p, ok := strings.Cut(key, "@")
		if !ok || g != group {
			continue
		}
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" && strings.HasPrefix(m, p) && len(p) > len(best) {
			best, conf = p, c
		}
	}
	if best != "" {
		return conf, best, true
	}
	c, ok := GroupTaskLimits[group]
	return c, "", ok
}

// GroupTaskLimitOverridePrefixes 返回某分组下所有模型级键的前缀。
// 分组通用名额计数时要排除这些前缀的任务，它们各自有独立名额。
func GroupTaskLimitOverridePrefixes(group string) []string {
	taskLimitMu.RLock()
	defer taskLimitMu.RUnlock()
	var out []string
	for key := range GroupTaskLimits {
		if g, p, ok := strings.Cut(key, "@"); ok && g == group {
			if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

// GroupTaskLimitsCopy 返回全部配置的副本（目录接口用）。
func GroupTaskLimitsCopy() map[string]TaskLimitConfig {
	taskLimitMu.RLock()
	defer taskLimitMu.RUnlock()
	out := make(map[string]TaskLimitConfig, len(GroupTaskLimits))
	for k, v := range GroupTaskLimits {
		out[k] = v
	}
	return out
}

// TaskLimitScopeMatches 判断模型是否在限制范围内。范围为空时不匹配任何模型（限制不生效）。
func TaskLimitScopeMatches(modelName string) bool {
	return scopeMatches(modelName, false)
}

// TaskLimitScopeMatchesOrAll 供队列展示接口使用：范围为空时展示全部异步任务。
func TaskLimitScopeMatchesOrAll(modelName string) bool {
	return scopeMatches(modelName, true)
}

func scopeMatches(modelName string, emptyMeansAll bool) bool {
	taskLimitMu.RLock()
	defer taskLimitMu.RUnlock()
	if len(TaskLimitScope) == 0 {
		return emptyMeansAll
	}
	m := strings.ToLower(strings.TrimSpace(modelName))
	for _, p := range TaskLimitScope {
		if strings.HasPrefix(m, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// TaskLimitScopeCopy 返回范围副本（队列接口按同一口径过滤任务）。
func TaskLimitScopeCopy() []string {
	taskLimitMu.RLock()
	defer taskLimitMu.RUnlock()
	out := make([]string, len(TaskLimitScope))
	copy(out, TaskLimitScope)
	return out
}
