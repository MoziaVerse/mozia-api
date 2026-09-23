package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAuditWorker(t *testing.T, entries []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		if r.URL.Path == "/v1/videos/audit" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(entries)
			return
		}
		http.NotFound(w, r)
	}))
}

func TestH3Probe_AuditSnapshot(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	h3ProbeNow = func() time.Time { return now }
	defer func() { h3ProbeNow = time.Now }()

	base := float64(now.Unix())
	srv := newAuditWorker(t, []map[string]any{
		{"id": "q1", "state": "queued", "accepted_at": base - 2400, "queued_at": base - 2400},
		{"id": "q2", "state": "queued", "accepted_at": base - 600, "queued_at": base - 600},
		{"id": "r1", "state": "running", "accepted_at": base - 900, "queued_at": base - 900, "dequeued_at": base - 300, "started_at": base - 300},
		{"id": "c1", "state": "completed", "accepted_at": base - 3000, "queued_at": base - 3000, "dequeued_at": base - 2900, "started_at": base - 2900, "finished_at": base - 2500},
		{"id": "c2", "state": "completed", "accepted_at": base - 2000, "queued_at": base - 2000, "dequeued_at": base - 1900, "started_at": base - 1900, "finished_at": base - 1300},
		{"id": "c3", "state": "completed", "accepted_at": base - 1500, "queued_at": base - 1500, "dequeued_at": base - 1400, "started_at": base - 1400, "finished_at": base - 900},
		{"id": "f1", "state": "failed", "accepted_at": base - 100, "finished_at": base - 50},
	})
	defer srv.Close()

	ch := &model.Channel{Id: 901, Type: constant.ChannelTypeMoziaH3, Name: "worker-a", Key: "test-key"}
	ch.BaseURL = &srv.URL

	snap, err := fetchWorkerSnapshot(context.Background(), ch, now)
	require.NoError(t, err)
	assert.True(t, snap.HasTimeline)
	assert.Equal(t, 2, snap.Queued)
	assert.Equal(t, 1, snap.Running)
	assert.Equal(t, base-2400, snap.OldestQueuedAt)
	assert.Equal(t, 3, snap.RunSamples)
	// 三个样本的执行时长：400 / 600 / 500 → 中位数 500
	assert.Equal(t, float64(500), snap.P50RunSeconds)
	assert.Len(t, snap.Active, 3)

	// 有 running 就不是假活，哪怕队头等了 40 分钟
	evaluateWorkerSuspect(snap, now, 30*time.Minute)
	assert.False(t, snap.Suspect)
}

func TestH3Probe_ListFallbackForOldBuild(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/videos/audit":
			// 旧构建把 audit 当成 video id
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"detail":"Video not found"}`))
		case "/v1/videos":
			_, _ = w.Write([]byte(`{"data":[
				{"id":"a","status":"queued","created_at":1799999000,"completed_at":null},
				{"id":"b","status":"running","created_at":1799998000,"completed_at":null},
				{"id":"c","status":"completed","created_at":1799990000,"completed_at":1799990600}
			],"object":"list"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ch := &model.Channel{Id: 902, Type: constant.ChannelTypeMoziaH3, Name: "worker-old", Key: "k"}
	ch.BaseURL = &srv.URL
	snap, err := fetchWorkerSnapshot(context.Background(), ch, now)
	require.NoError(t, err)
	assert.False(t, snap.HasTimeline)
	assert.Equal(t, 1, snap.Queued)
	assert.Equal(t, 1, snap.Running)
	assert.Equal(t, float64(1799999000), snap.OldestQueuedAt)
	assert.Equal(t, float64(600), snap.P50RunSeconds)
}

func TestH3Probe_SuspectDetection(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	snap := &WorkerQueueSnapshot{Queued: 3, Running: 0, OldestQueuedAt: float64(now.Unix() - 31*60)}
	evaluateWorkerSuspect(snap, now, 30*time.Minute)
	assert.True(t, snap.Suspect)
	assert.Contains(t, snap.SuspectReason, "3 条任务排队")

	fresh := &WorkerQueueSnapshot{Queued: 3, Running: 0, OldestQueuedAt: float64(now.Unix() - 5*60)}
	evaluateWorkerSuspect(fresh, now, 30*time.Minute)
	assert.False(t, fresh.Suspect)
}

func TestH3Probe_BackfillStartTime(t *testing.T) {
	require.NoError(t, model.DB.Where("channel_id = ?", 903).Delete(&model.Task{}).Error)
	queued := &model.Task{TaskID: "pub-1", Platform: constant.TaskPlatform("207"), ChannelId: 903, UserId: 1, Status: model.TaskStatusQueued, Progress: "20%", SubmitTime: 1000}
	queued.PrivateData.UpstreamTaskID = "up-running"
	require.NoError(t, queued.Insert())
	still := &model.Task{TaskID: "pub-2", Platform: constant.TaskPlatform("207"), ChannelId: 903, UserId: 1, Status: model.TaskStatusQueued, Progress: "20%", SubmitTime: 1000}
	still.PrivateData.UpstreamTaskID = "up-queued"
	require.NoError(t, still.Insert())
	done := &model.Task{TaskID: "pub-3", Platform: constant.TaskPlatform("207"), ChannelId: 903, UserId: 1, Status: model.TaskStatusSuccess, Progress: "100%", SubmitTime: 1000}
	done.PrivateData.UpstreamTaskID = "up-done"
	require.NoError(t, done.Insert())

	snap := &WorkerQueueSnapshot{ChannelId: 903, SampledAt: 5000, Active: map[string]WorkerAuditEntry{
		"up-running": {ID: "up-running", State: "running", DequeuedAt: 4200, StartedAt: 4201},
		"up-queued":  {ID: "up-queued", State: "queued", QueuedAt: 1000},
		"up-done":    {ID: "up-done", State: "running", DequeuedAt: 4300},
	}}
	backfillTaskTimeline(context.Background(), 903, snap)

	load := func(taskID string) model.Task {
		var got model.Task
		require.NoError(t, model.DB.Where("task_id = ?", taskID).First(&got).Error)
		return got
	}
	got := load("pub-1")
	assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), got.Status)
	assert.Equal(t, int64(4200), got.StartTime)

	got = load("pub-2")
	assert.Equal(t, model.TaskStatus(model.TaskStatusQueued), got.Status)
	assert.Equal(t, int64(0), got.StartTime)

	// 终态任务不被回填改动
	got = load("pub-3")
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), got.Status)
	assert.Equal(t, int64(0), got.StartTime)
}

func TestH3Probe_AlertOnce(t *testing.T) {
	calls := 0
	h3ProbeAlertFunc = func(ctx context.Context, ch *model.Channel, snap *WorkerQueueSnapshot, msg string) { calls++ }
	defer func() { h3ProbeAlertFunc = sendH3WorkerAlert }()
	ch := &model.Channel{Id: 904, Name: "w"}
	bad := &WorkerQueueSnapshot{ChannelId: 904, Suspect: true, SuspectReason: "x"}
	notifyWorkerSuspect(context.Background(), ch, bad)
	notifyWorkerSuspect(context.Background(), ch, bad)
	assert.Equal(t, 1, calls, "同一 channel 持续假活只告警一次")
	notifyWorkerSuspect(context.Background(), ch, &WorkerQueueSnapshot{ChannelId: 904})
	notifyWorkerSuspect(context.Background(), ch, bad)
	assert.Equal(t, 2, calls, "恢复后再次假活重新告警")
}

// 停用 / 删除的 channel 本轮没枚举到，其快照与告警状态必须清掉，否则池子统计里永远有幽灵 worker
func TestH3Probe_PruneStaleSnapshots(t *testing.T) {
	h3SnapshotMu.Lock()
	h3SnapshotStore = map[int]*WorkerQueueSnapshot{905: {ChannelId: 905}, 906: {ChannelId: 906}}
	h3SuspectAlerted = map[int]bool{906: true}
	h3SnapshotMu.Unlock()
	t.Cleanup(func() { pruneWorkerSnapshots(map[int]bool{}) })
	pruneWorkerSnapshots(map[int]bool{905: true})
	assert.NotNil(t, GetWorkerQueueSnapshot(905))
	assert.Nil(t, GetWorkerQueueSnapshot(906))
	h3SnapshotMu.RLock()
	_, alerted := h3SuspectAlerted[906]
	h3SnapshotMu.RUnlock()
	assert.False(t, alerted)
}

// worker 完全连不上：超过假活阈值后走同一条告警管道，只报一次
func TestH3Probe_AlertOnSustainedFetchFailure(t *testing.T) {
	calls := 0
	h3ProbeAlertFunc = func(ctx context.Context, ch *model.Channel, snap *WorkerQueueSnapshot, msg string) { calls++ }
	defer func() { h3ProbeAlertFunc = sendH3WorkerAlert }()
	base := time.Date(2026, 9, 23, 10, 0, 0, 0, time.UTC)
	h3ProbeNow = func() time.Time { return base }
	defer func() { h3ProbeNow = time.Now }()
	t.Setenv("H3_WORKER_STALE_MINUTES", "5")
	h3SnapshotMu.Lock()
	delete(h3SnapshotStore, 907)
	delete(h3SuspectAlerted, 907)
	h3SnapshotMu.Unlock()
	t.Cleanup(func() { pruneWorkerSnapshots(map[int]bool{}) })

	ch := &model.Channel{Id: 907, Name: "dead", Type: constant.ChannelTypeMoziaH3, BaseURL: strPtr("http://127.0.0.1:1")}
	probeH3Channel(context.Background(), ch)
	assert.Equal(t, 0, calls, "刚失败不告警")
	h3ProbeNow = func() time.Time { return base.Add(6 * time.Minute) }
	probeH3Channel(context.Background(), ch)
	probeH3Channel(context.Background(), ch)
	assert.Equal(t, 1, calls, "持续不可达超过阈值告警一次")
	snap := GetWorkerQueueSnapshot(907)
	assert.True(t, snap.Suspect)
	assert.Equal(t, base.Unix(), snap.FailingSince)
}

func strPtr(s string) *string { return &s }
