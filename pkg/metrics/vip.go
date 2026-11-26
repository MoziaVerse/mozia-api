package metrics

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

const (
	defaultMaxTrackedModels = 500
	defaultMaxTrackedGroups = 100
	overflowLabelValue      = "__other__"
)

// VIPConfig holds parsed VIP watchlists merged from environment variables and
// hot-reloadable database options.
type VIPConfig struct {
	mu           sync.RWMutex
	envUsers     map[int]string
	envGroups    map[string]struct{}
	optUsers     map[int]string
	optGroups    map[string]struct{}
	mergedUsers  map[int]string
	mergedGroups map[string]struct{}
}

func newVIPConfigFromEnv() *VIPConfig {
	cfg := &VIPConfig{
		envUsers:     parseVIPUsers(os.Getenv("MONITOR_VIP_USERS")),
		envGroups:    parseVIPGroups(os.Getenv("MONITOR_VIP_GROUPS")),
		optUsers:     make(map[int]string),
		optGroups:    make(map[string]struct{}),
		mergedUsers:  make(map[int]string),
		mergedGroups: make(map[string]struct{}),
	}
	cfg.rebuildLocked()
	return cfg
}

// SetEnvConfig overrides the environment-derived VIP watchlist (primarily for tests).
func (v *VIPConfig) SetEnvConfig(usersSpec, groupsSpec string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.envUsers = parseVIPUsers(usersSpec)
	v.envGroups = parseVIPGroups(groupsSpec)
	v.rebuildLocked()
}

// SetDynamicUsers updates the DB-option VIP user list (e.g. MonitorVIPUsers).
func (v *VIPConfig) SetDynamicUsers(usersSpec string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.optUsers = parseVIPUsers(usersSpec)
	v.rebuildLocked()
}

// SetDynamicGroups updates the DB-option VIP group list (e.g. MonitorVIPGroups).
func (v *VIPConfig) SetDynamicGroups(groupsSpec string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.optGroups = parseVIPGroups(groupsSpec)
	v.rebuildLocked()
}

func (v *VIPConfig) rebuildLocked() {
	users := make(map[int]string, len(v.envUsers)+len(v.optUsers))
	for id, alias := range v.envUsers {
		users[id] = alias
	}
	for id, alias := range v.optUsers {
		if existing, exists := users[id]; exists && alias == "" {
			users[id] = existing
			continue
		}
		users[id] = alias
	}
	groups := make(map[string]struct{}, len(v.envGroups)+len(v.optGroups))
	for g := range v.envGroups {
		groups[g] = struct{}{}
	}
	for g := range v.optGroups {
		groups[g] = struct{}{}
	}
	v.mergedUsers = users
	v.mergedGroups = groups
}

// Resolve returns the bounded customer and user_id labels when a request belongs
// to a watched key customer (matched by explicit user ID first, then VIP group).
func (v *VIPConfig) Resolve(userID int, username, group string) (customer string, userIDLabel string, ok bool) {
	if v == nil {
		return "", "", false
	}
	v.mu.RLock()
	defer v.mu.RUnlock()

	if userID > 0 {
		if alias, exists := v.mergedUsers[userID]; exists {
			idStr := strconv.Itoa(userID)
			if alias != "" {
				return sanitizeLabelValue(alias, 64), idStr, true
			}
			if name := sanitizeLabelValue(username, 48); name != "" {
				return name, idStr, true
			}
			return "user_" + idStr, idStr, true
		}
	}

	normalizedGroup := strings.TrimSpace(group)
	if normalizedGroup != "" {
		if _, exists := v.mergedGroups[strings.ToLower(normalizedGroup)]; exists {
			return "group:" + sanitizeLabelValue(normalizedGroup, 48), "group", true
		}
	}
	return "", "", false
}

// parseVIPUsers parses comma/newline/semicolon-separated specs like:
//
//	"6218, 7001:AcmeCorp, 8002=BigClient"
func parseVIPUsers(spec string) map[int]string {
	result := make(map[int]string)
	for _, token := range splitWatchlist(spec) {
		idPart := token
		alias := ""
		if idx := strings.IndexAny(token, ":="); idx >= 0 {
			idPart = strings.TrimSpace(token[:idx])
			alias = strings.TrimSpace(token[idx+1:])
		}
		id, err := strconv.Atoi(idPart)
		if err != nil || id <= 0 {
			continue
		}
		result[id] = sanitizeLabelValue(alias, 64)
	}
	return result
}

func parseVIPGroups(spec string) map[string]struct{} {
	result := make(map[string]struct{})
	for _, token := range splitWatchlist(spec) {
		g := strings.ToLower(strings.TrimSpace(token))
		if g != "" {
			result[g] = struct{}{}
		}
	}
	return result
}

func splitWatchlist(spec string) []string {
	return strings.FieldsFunc(spec, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n' || r == '\r'
	})
}

func sanitizeLabelValue(raw string, maxLen int) string {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return ""
	}
	cleaned = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, cleaned)
	if maxLen > 0 && len(cleaned) > maxLen {
		cleaned = cleaned[:maxLen]
	}
	return cleaned
}

// labelLimiter bounds Prometheus label cardinality so invalid client inputs
// (such as random model names) cannot create unbounded time series.
type labelLimiter struct {
	mu      sync.RWMutex
	maxSize int
	seen    map[string]struct{}
}

func newLabelLimiter(maxSize int) *labelLimiter {
	if maxSize <= 0 {
		maxSize = defaultMaxTrackedModels
	}
	return &labelLimiter{
		maxSize: maxSize,
		seen:    make(map[string]struct{}),
	}
}

func (l *labelLimiter) Normalize(raw, fallback string) string {
	val := sanitizeLabelValue(raw, 80)
	if val == "" {
		return fallback
	}
	l.mu.RLock()
	_, ok := l.seen[val]
	atCap := len(l.seen) >= l.maxSize
	l.mu.RUnlock()
	if ok {
		return val
	}
	if atCap {
		return overflowLabelValue
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.seen[val]; exists {
		return val
	}
	if len(l.seen) >= l.maxSize {
		return overflowLabelValue
	}
	l.seen[val] = struct{}{}
	return val
}

func parsePositiveIntEnv(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func formatChannelID(id int) string {
	if id <= 0 {
		return "0"
	}
	return fmt.Sprintf("%d", id)
}
