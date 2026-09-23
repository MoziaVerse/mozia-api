package service

// H3 worker 状态采集。
//
// 自托管 H3（channel type 207 / 208）的 worker 自带 FIFO，网关"收到就转发"后
// 看不见队列：tasks.start_time 只在轮询恰好撞见 running 时才写（生产 76% 为空），
// worker 假活时任务永远 QUEUED，要等 TaskTimeoutMinutes 才退款。
//
// 这里每隔几秒问一遍每个启用的 H3 channel：
//   - 新构建 worker 提供 GET /v1/videos/audit，带 accepted/queued/dequeued/started/finished
//     时间线，是排队真相；
//   - 旧构建只有 GET /v1/videos 列表（status + created_at + completed_at），退化使用。
// 采集结果落成 WorkerQueueSnapshot（内存 + Redis），供派发、并发检查、队列接口读取；
// 同时把 dequeued_at 回填到 tasks.start_time，并把长期不动的队列判为假活告警。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"

	"github.com/bytedance/gopkg/util/gopool"
)

// WorkerAuditEntry 是 worker 视角的一条任务时间线（秒级 unix 时间戳，0 表示未发生）。
type WorkerAuditEntry struct {
	ID         string  `json:"id"`
	State      string  `json:"state"`
	AcceptedAt float64 `json:"accepted_at"`
	QueuedAt   float64 `json:"queued_at"`
	DequeuedAt float64 `json:"dequeued_at"`
	StartedAt  float64 `json:"started_at"`
	FinishedAt float64 `json:"finished_at"`
}

// WorkerQueueSnapshot 是一个 channel（= 一台 worker）在采样时刻的队列画像。
type WorkerQueueSnapshot struct {
	ChannelId      int     `json:"channel_id"`
	ChannelType    int     `json:"channel_type"`
	SampledAt      int64   `json:"sampled_at"`
	Running        int     `json:"running"`
	Queued         int     `json:"queued"`
	OldestQueuedAt float64 `json:"oldest_queued_at"`
	// 最近完成任务的真实执行时长中位数（秒），样本不足为 0。
	P50RunSeconds float64 `json:"p50_run_seconds"`
	RunSamples    int     `json:"run_samples"`
	// 是否来自 audit 接口（旧构建只有列表，时间线精度低一档）。
	HasTimeline bool `json:"has_timeline"`
	// 假活判定：队列非空、无 running、最早排队超过阈值。
	Suspect       bool   `json:"suspect"`
	SuspectReason string `json:"suspect_reason,omitempty"`
	// 采集失败时保留上一次画像，只标记错误。
	LastError string `json:"last_error,omitempty"`
	// 连续采集失败的起点（unix 秒），恢复后清零；持续超过假活阈值则按假活告警
	FailingSince int64 `json:"failing_since,omitempty"`
	// 仍在 worker 上未终结的任务，key 为上游 video id。
	Active map[string]WorkerAuditEntry `json:"active"`
}

const (
	h3ProbeDefaultInterval = 10 * time.Second
	h3ProbeDefaultStale    = 30 * time.Minute
	h3ProbeAuditLimit      = 300
	h3ProbeRedisKeyPrefix  = "h3:worker:snapshot:"
	h3ProbeRedisTTL        = 120 * time.Second
	h3ProbeHTTPTimeout     = 8 * time.Second
	h3ProbeRunSampleWindow = 50
)

var (
	h3ProbeOnce    sync.Once
	h3ProbeRunning atomic.Bool

	h3SnapshotMu    sync.RWMutex
	h3SnapshotStore = map[int]*WorkerQueueSnapshot{}

	// 上一轮已告警的 channel，避免每 10 秒重复告警；恢复后清除。
	h3SuspectAlerted = map[int]bool{}

	// 便于测试替换。
	h3ProbeNow        = time.Now
	h3ProbeHTTPClient = &http.Client{Timeout: h3ProbeHTTPTimeout}
	h3ProbeAlertFunc  = sendH3WorkerAlert
)

// WorkerSnapshotFetcher 是「按 channel 类型」的队列采集实现。新增上游只需注册一个实现，
// 采集循环、快照存储、假活判定、回填、队列接口全部复用。
type WorkerSnapshotFetcher func(ctx context.Context, ch *model.Channel, now time.Time) (*WorkerQueueSnapshot, error)

var workerProbeRegistry = map[int]WorkerSnapshotFetcher{
	// SGLang Diffusion worker：audit 时间线，旧构建回退列表
	constant.ChannelTypeMoziaH3: fetchWorkerSnapshot,
	// VDN（208）是另一套 multipart 服务，没有 audit / 列表接口（生产实测 404），未注册即不采集
}

// H3WorkerChannelTypes 返回已注册采集的 channel 类型（升序，便于日志稳定）。
func H3WorkerChannelTypes() []int {
	out := make([]int, 0, len(workerProbeRegistry))
	for t := range workerProbeRegistry {
		out = append(out, t)
	}
	sort.Ints(out)
	return out
}

func h3ProbeInterval() time.Duration {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("H3_WORKER_PROBE_SECONDS"))); err == nil && v > 0 {
		return time.Duration(v) * time.Second
	}
	return h3ProbeDefaultInterval
}

func h3ProbeStaleAfter() time.Duration {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv("H3_WORKER_STALE_MINUTES"))); err == nil && v > 0 {
		return time.Duration(v) * time.Minute
	}
	return h3ProbeDefaultStale
}

// StartH3WorkerProbeTask 在主节点上启动采集循环。
func StartH3WorkerProbeTask() {
	h3ProbeOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		if strings.EqualFold(strings.TrimSpace(os.Getenv("H3_WORKER_PROBE_DISABLED")), "true") {
			return
		}
		interval := h3ProbeInterval()
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("h3 worker probe started: interval=%s stale_after=%s", interval, h3ProbeStaleAfter()))
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			RunH3WorkerProbeOnce(context.Background())
			for range ticker.C {
				RunH3WorkerProbeOnce(context.Background())
			}
		})
	})
}

// RunH3WorkerProbeOnce 采集一轮所有启用的 H3 channel。可重入保护：上一轮未完成则跳过。
func RunH3WorkerProbeOnce(ctx context.Context) {
	if !h3ProbeRunning.CompareAndSwap(false, true) {
		return
	}
	defer h3ProbeRunning.Store(false)

	seen := map[int]bool{}
	for _, channelType := range H3WorkerChannelTypes() {
		channels, err := model.GetEnabledChannelsByTypeAndGroup(channelType, "")
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("h3 worker probe: list channels type=%d failed: %v", channelType, err))
			// 列表都拿不到时不清理，避免把有效快照误删
			return
		}
		for _, ch := range channels {
			if ctx.Err() != nil {
				return
			}
			seen[ch.Id] = true
			probeH3Channel(ctx, ch)
		}
	}
	pruneWorkerSnapshots(seen)
}

// pruneWorkerSnapshots 丢掉本轮没有枚举到的 channel（已停用 / 已删除）的快照与告警状态，
// 否则它们会永远留在池子统计里变成幽灵 worker。
func pruneWorkerSnapshots(seen map[int]bool) {
	h3SnapshotMu.Lock()
	defer h3SnapshotMu.Unlock()
	for id := range h3SnapshotStore {
		if !seen[id] {
			delete(h3SnapshotStore, id)
			delete(h3SuspectAlerted, id)
		}
	}
}

func probeH3Channel(ctx context.Context, ch *model.Channel) {
	now := h3ProbeNow()
	fetch := workerProbeRegistry[ch.Type]
	if fetch == nil {
		return
	}
	snap, err := fetch(ctx, ch, now)
	if err != nil {
		prev := GetWorkerQueueSnapshot(ch.Id)
		if prev == nil {
			prev = &WorkerQueueSnapshot{ChannelId: ch.Id, ChannelType: ch.Type, Active: map[string]WorkerAuditEntry{}}
		}
		// 每 10 秒一轮，失败只在「从正常变为失败」时记一次，恢复时再记一次，避免刷屏
		if prev.LastError == "" {
			logger.LogWarn(ctx, fmt.Sprintf("h3 worker probe: channel #%d (%s) fetch failed: %v", ch.Id, ch.Name, err))
			prev.FailingSince = now.Unix()
		}
		prev.LastError = err.Error()
		// 持续连不上（超过假活阈值）与假活同等对待：走同一条告警管道，只报一次，恢复后重置
		if prev.FailingSince > 0 && now.Sub(time.Unix(prev.FailingSince, 0)) >= h3ProbeStaleAfter() {
			prev.Suspect = true
			prev.SuspectReason = fmt.Sprintf("worker 连续 %s 不可达：%v", now.Sub(time.Unix(prev.FailingSince, 0)).Round(time.Minute), err)
		}
		storeWorkerSnapshot(prev)
		notifyWorkerSuspect(ctx, ch, prev)
		return
	}
	if prev := GetWorkerQueueSnapshot(ch.Id); prev != nil && prev.LastError != "" {
		logger.LogInfo(ctx, fmt.Sprintf("h3 worker probe: channel #%d (%s) recovered", ch.Id, ch.Name))
	}
	evaluateWorkerSuspect(snap, now, h3ProbeStaleAfter())
	storeWorkerSnapshot(snap)
	backfillTaskTimeline(ctx, ch.Id, snap)
	notifyWorkerSuspect(ctx, ch, snap)
}

// fetchWorkerSnapshot 优先走 audit，旧构建回退到列表。
func fetchWorkerSnapshot(ctx context.Context, ch *model.Channel, now time.Time) (*WorkerQueueSnapshot, error) {
	base := h3WorkerAPIBase(ch.GetBaseURL())
	entries, err := fetchWorkerAudit(ctx, base, ch.Key)
	hasTimeline := true
	if err != nil {
		if !isAuditUnsupported(err) {
			return nil, err
		}
		hasTimeline = false
		entries, err = fetchWorkerVideoList(ctx, base, ch.Key)
		if err != nil {
			return nil, err
		}
	}
	snap := buildWorkerSnapshot(ch, entries, now)
	snap.HasTimeline = hasTimeline
	return snap, nil
}

type auditUnsupportedError struct{ status int }

func (e *auditUnsupportedError) Error() string {
	return fmt.Sprintf("audit endpoint unsupported (status %d)", e.status)
}

func isAuditUnsupported(err error) bool {
	_, ok := err.(*auditUnsupportedError)
	return ok
}

func h3WorkerAPIBase(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL
	}
	return baseURL + "/v1"
}

func h3WorkerGet(ctx context.Context, rawURL, key string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := h3ProbeHTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// fetchWorkerAudit 拉 GET /v1/videos/audit。旧构建把 "audit" 当 video id，返回 404
// {"detail":"Video not found"}，据此判定不支持。
func fetchWorkerAudit(ctx context.Context, base, key string) ([]WorkerAuditEntry, error) {
	q := url.Values{}
	q.Set("limit", strconv.Itoa(h3ProbeAuditLimit))
	body, status, err := h3WorkerGet(ctx, base+"/videos/audit?"+q.Encode(), key)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound || status == http.StatusMethodNotAllowed {
		return nil, &auditUnsupportedError{status: status}
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("audit status %d: %s", status, truncateForLog(body))
	}
	trimmed := bytes.TrimSpace(body)
	if !bytes.HasPrefix(trimmed, []byte("[")) {
		// 旧构建可能返回 200 + {"detail": ...}
		return nil, &auditUnsupportedError{status: status}
	}
	var raw []struct {
		ID         string   `json:"id"`
		State      string   `json:"state"`
		AcceptedAt *float64 `json:"accepted_at"`
		QueuedAt   *float64 `json:"queued_at"`
		DequeuedAt *float64 `json:"dequeued_at"`
		StartedAt  *float64 `json:"started_at"`
		FinishedAt *float64 `json:"finished_at"`
	}
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return nil, fmt.Errorf("decode audit: %w", err)
	}
	entries := make([]WorkerAuditEntry, 0, len(raw))
	for _, r := range raw {
		entries = append(entries, WorkerAuditEntry{
			ID:         r.ID,
			State:      strings.ToLower(strings.TrimSpace(r.State)),
			AcceptedAt: deref(r.AcceptedAt),
			QueuedAt:   deref(r.QueuedAt),
			DequeuedAt: deref(r.DequeuedAt),
			StartedAt:  deref(r.StartedAt),
			FinishedAt: deref(r.FinishedAt),
		})
	}
	return entries, nil
}

// fetchWorkerVideoList 是旧构建的退化路径：只有 status / created_at / completed_at。
func fetchWorkerVideoList(ctx context.Context, base, key string) ([]WorkerAuditEntry, error) {
	q := url.Values{}
	q.Set("limit", strconv.Itoa(h3ProbeAuditLimit))
	body, status, err := h3WorkerGet(ctx, base+"/videos?"+q.Encode(), key)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("videos list status %d: %s", status, truncateForLog(body))
	}
	var raw struct {
		Data []struct {
			ID          string `json:"id"`
			Status      string `json:"status"`
			CreatedAt   int64  `json:"created_at"`
			CompletedAt *int64 `json:"completed_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode videos list: %w", err)
	}
	entries := make([]WorkerAuditEntry, 0, len(raw.Data))
	for _, r := range raw.Data {
		e := WorkerAuditEntry{
			ID:         r.ID,
			State:      strings.ToLower(strings.TrimSpace(r.Status)),
			AcceptedAt: float64(r.CreatedAt),
			QueuedAt:   float64(r.CreatedAt),
		}
		if r.CompletedAt != nil {
			e.FinishedAt = float64(*r.CompletedAt)
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func isWorkerRunningState(state string) bool {
	switch state {
	case "running", "in_progress", "started", "conditioning", "generating", "waiting_decode", "decoding", "processing":
		return true
	}
	return false
}

func isWorkerTerminalState(state string) bool {
	switch state {
	case "completed", "failed", "cancelled", "canceled":
		return true
	}
	return false
}

// buildWorkerSnapshot 由 worker 返回的条目算出队列画像。
func buildWorkerSnapshot(ch *model.Channel, entries []WorkerAuditEntry, now time.Time) *WorkerQueueSnapshot {
	snap := &WorkerQueueSnapshot{
		ChannelId:   ch.Id,
		ChannelType: ch.Type,
		SampledAt:   now.Unix(),
		Active:      map[string]WorkerAuditEntry{},
	}
	type sample struct {
		finished float64
		run      float64
	}
	var samples []sample
	for _, e := range entries {
		switch {
		case e.State == "queued":
			snap.Queued++
			snap.Active[e.ID] = e
			qa := e.QueuedAt
			if qa == 0 {
				qa = e.AcceptedAt
			}
			if qa > 0 && (snap.OldestQueuedAt == 0 || qa < snap.OldestQueuedAt) {
				snap.OldestQueuedAt = qa
			}
		case isWorkerRunningState(e.State):
			snap.Running++
			snap.Active[e.ID] = e
		case e.State == "completed":
			start := e.StartedAt
			if start == 0 {
				start = e.DequeuedAt
			}
			if start == 0 {
				// 旧构建没有开始时间，只能用 accepted → finished，含排队。
				start = e.AcceptedAt
			}
			if start > 0 && e.FinishedAt > start {
				samples = append(samples, sample{finished: e.FinishedAt, run: e.FinishedAt - start})
			}
		}
	}
	if len(samples) > 0 {
		sort.Slice(samples, func(i, j int) bool { return samples[i].finished > samples[j].finished })
		if len(samples) > h3ProbeRunSampleWindow {
			samples = samples[:h3ProbeRunSampleWindow]
		}
		runs := make([]float64, len(samples))
		for i, s := range samples {
			runs[i] = s.run
		}
		sort.Float64s(runs)
		snap.P50RunSeconds = runs[len(runs)/2]
		snap.RunSamples = len(runs)
	}
	return snap
}

// evaluateWorkerSuspect 假活判定：有人排队、没人在跑、最早的那条等了太久。
func evaluateWorkerSuspect(snap *WorkerQueueSnapshot, now time.Time, staleAfter time.Duration) {
	snap.Suspect = false
	snap.SuspectReason = ""
	if snap.Queued == 0 || snap.Running > 0 || snap.OldestQueuedAt == 0 {
		return
	}
	waited := now.Sub(time.Unix(int64(snap.OldestQueuedAt), 0))
	if waited >= staleAfter {
		snap.Suspect = true
		snap.SuspectReason = fmt.Sprintf("%d 条任务排队、无运行中任务，最早一条已等待 %d 分钟", snap.Queued, int(waited.Minutes()))
	}
}

// backfillTaskTimeline 把 worker 的 dequeued/started 回填到网关任务：
// 只对仍在 SUBMITTED/QUEUED 的任务做 CAS 更新，终态由轮询负责结算。
func backfillTaskTimeline(ctx context.Context, channelId int, snap *WorkerQueueSnapshot) {
	if len(snap.Active) == 0 {
		return
	}
	tasks := model.GetUnfinishedTasksByChannel(channelId, h3ProbeAuditLimit)
	for _, t := range tasks {
		if t.Status != model.TaskStatusQueued && t.Status != model.TaskStatusSubmitted {
			continue
		}
		e, ok := snap.Active[t.GetUpstreamTaskID()]
		if !ok || !isWorkerRunningState(e.State) {
			continue
		}
		startAt := e.DequeuedAt
		if startAt == 0 {
			startAt = e.StartedAt
		}
		if startAt == 0 {
			startAt = float64(snap.SampledAt)
		}
		won, err := model.MarkTaskStartedByID(t.ID, int64(startAt))
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("h3 worker probe: backfill start_time for task %s failed: %v", t.TaskID, err))
			continue
		}
		if won {
			logger.LogInfo(ctx, fmt.Sprintf("h3 worker probe: task %s started on channel #%d at %d", t.TaskID, channelId, int64(startAt)))
		}
	}
}

func notifyWorkerSuspect(ctx context.Context, ch *model.Channel, snap *WorkerQueueSnapshot) {
	h3SnapshotMu.Lock()
	alreadyAlerted := h3SuspectAlerted[ch.Id]
	if snap.Suspect {
		h3SuspectAlerted[ch.Id] = true
	} else {
		delete(h3SuspectAlerted, ch.Id)
	}
	h3SnapshotMu.Unlock()

	if !snap.Suspect || alreadyAlerted {
		return
	}
	msg := fmt.Sprintf("H3 worker 疑似假活：channel #%d %s（%s）\n%s\n请检查该机并考虑停用 channel。", ch.Id, ch.Name, ch.GetBaseURL(), snap.SuspectReason)
	logger.LogError(ctx, msg)
	h3ProbeAlertFunc(ctx, ch, snap, msg)
}

// sendH3WorkerAlert 默认走飞书自定义机器人 webhook（H3_WORKER_ALERT_WEBHOOK），
// 没配则退到通知 root 用户。
func sendH3WorkerAlert(ctx context.Context, ch *model.Channel, snap *WorkerQueueSnapshot, msg string) {
	webhook := strings.TrimSpace(os.Getenv("H3_WORKER_ALERT_WEBHOOK"))
	if webhook == "" {
		NotifyRootUser("h3_worker_suspect", "H3 worker 疑似假活", msg)
		return
	}
	payload, _ := json.Marshal(map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": msg},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(payload))
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("h3 worker alert: build request failed: %v", err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h3ProbeHTTPClient.Do(req)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("h3 worker alert: send failed: %v", err))
		return
	}
	resp.Body.Close()
}

// ---- snapshot store ----

func storeWorkerSnapshot(snap *WorkerQueueSnapshot) {
	h3SnapshotMu.Lock()
	h3SnapshotStore[snap.ChannelId] = snap
	h3SnapshotMu.Unlock()
	if common.RedisEnabled {
		if b, err := json.Marshal(snap); err == nil {
			_ = common.RedisSet(h3ProbeRedisKeyPrefix+strconv.Itoa(snap.ChannelId), string(b), h3ProbeRedisTTL)
		}
	}
}

// GetWorkerQueueSnapshot 读一个 channel 的最新画像；本节点没有则从 Redis 取（从节点场景）。
func GetWorkerQueueSnapshot(channelId int) *WorkerQueueSnapshot {
	h3SnapshotMu.RLock()
	snap, ok := h3SnapshotStore[channelId]
	h3SnapshotMu.RUnlock()
	if ok {
		return snap
	}
	if !common.RedisEnabled {
		return nil
	}
	raw, err := common.RedisGet(h3ProbeRedisKeyPrefix + strconv.Itoa(channelId))
	if err != nil || raw == "" {
		return nil
	}
	var s WorkerQueueSnapshot
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return nil
	}
	return &s
}

// ListWorkerQueueSnapshots 返回本节点缓存的全部画像（看板 / 队列接口用）。
func ListWorkerQueueSnapshots() []*WorkerQueueSnapshot {
	h3SnapshotMu.RLock()
	defer h3SnapshotMu.RUnlock()
	out := make([]*WorkerQueueSnapshot, 0, len(h3SnapshotStore))
	for _, s := range h3SnapshotStore {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ChannelId < out[j].ChannelId })
	return out
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func truncateForLog(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}
