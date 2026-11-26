package metrics

import (
	"database/sql"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// RedisPoolStats is a dependency-free projection of go-redis PoolStats.
type RedisPoolStats struct {
	Hits       uint32
	Misses     uint32
	Timeouts   uint32
	TotalConns uint32
	IdleConns  uint32
	StaleConns uint32
}

// ChannelCacheSnapshot holds a point-in-time summary of channels in memory cache.
type ChannelCacheSnapshot struct {
	StatusCounts        map[string]int
	ModelHealthyChannel map[string]map[string]int // group -> model -> enabled channel count
}

// RuntimeProviders supplies live gauges during Prometheus scrapes without
// introducing import cycles between pkg/metrics and model/middleware/common.
type RuntimeProviders struct {
	ActiveConnections   func() int64
	DBPoolStats         func() map[string]sql.DBStats
	RedisPoolStats      func() (RedisPoolStats, bool)
	BatchUpdatePending  func() map[string]int
	ChannelCacheSummary func() (ChannelCacheSnapshot, bool)
}

// RequestOutcomeSample records the final outcome of a relay request.
type RequestOutcomeSample struct {
	Model       string
	Group       string
	UserID      int
	Username    string
	Outcome     string // success | error | cancelled
	ErrorClass  string // none | client_4xx | gateway_5xx | upstream_429 | upstream_5xx | upstream_other | stream_error | client_cancelled | panic
	StatusCode  int
	IsStream    bool
	Duration    time.Duration
}

// RelayPerfSample records token throughput, latency, and quota usage on completion.
type RelayPerfSample struct {
	Model            string
	Group            string
	UserID           int
	Username         string
	IsStream         bool
	HasTTFT          bool
	TTFT             time.Duration
	GenerationTime   time.Duration
	PromptTokens     int
	CompletionTokens int
	QuotaConsumed    int
}

// StreamOutcomeSample records the terminal status of a streamed relay response.
type StreamOutcomeSample struct {
	Model     string
	Group     string
	UserID    int
	Username  string
	Status    string // ok | interrupted | client_gone
	EndReason string
}

// Recorder owns the Prometheus registry, metric vectors, VIP matcher, and
// runtime scrape collectors.
type Recorder struct {
	registry      *prometheus.Registry
	vip           *VIPConfig
	modelLimiter  *labelLimiter
	groupLimiter  *labelLimiter
	ready         atomic.Int64
	providersMu   sync.RWMutex
	providers     RuntimeProviders
	buildInfoOnce sync.Once

	buildInfo          *prometheus.GaugeVec
	relayRequests      *prometheus.CounterVec
	relayDuration      *prometheus.HistogramVec
	channelAttempts    *prometheus.CounterVec
	channelAutoDisable *prometheus.CounterVec
	ttftSeconds        *prometheus.HistogramVec
	tpsHistogram       *prometheus.HistogramVec
	gatewayOverhead    *prometheus.HistogramVec
	streamOutcomes     *prometheus.CounterVec
	tokensTotal        *prometheus.CounterVec
	quotaConsumed      *prometheus.CounterVec
	billingErrors      *prometheus.CounterVec
	paymentWebhooks    *prometheus.CounterVec

	vipRequests       *prometheus.CounterVec
	vipTTFTSeconds    *prometheus.HistogramVec
	vipTPSHistogram   *prometheus.HistogramVec
	vipStreamOutcomes *prometheus.CounterVec
	vipTokensTotal    *prometheus.CounterVec
	vipQuotaConsumed  *prometheus.CounterVec
}

var (
	descInstanceReady = prometheus.NewDesc(
		"mozia_instance_ready",
		"Whether this mozia-api instance is ready to serve traffic (1=ready, 0=draining/unready).",
		nil, nil,
	)
	descActiveConns = prometheus.NewDesc(
		"mozia_http_active_connections",
		"Current number of active in-flight HTTP/SSE connections on this instance.",
		nil, nil,
	)
	descDBPoolConns = prometheus.NewDesc(
		"mozia_db_pool_connections",
		"Database connection pool counts by database (main/log) and state (max_open/open/in_use/idle).",
		[]string{"db", "state"}, nil,
	)
	descDBPoolWaitCount = prometheus.NewDesc(
		"mozia_db_pool_wait_count_total",
		"Cumulative number of connections waited for from the database pool.",
		[]string{"db"}, nil,
	)
	descDBPoolWaitSeconds = prometheus.NewDesc(
		"mozia_db_pool_wait_duration_seconds_total",
		"Cumulative time blocked waiting for a new database connection, in seconds.",
		[]string{"db"}, nil,
	)
	descRedisPoolConns = prometheus.NewDesc(
		"mozia_redis_pool_connections",
		"Redis client pool connection counts by state (total/idle/stale).",
		[]string{"state"}, nil,
	)
	descRedisPoolTimeouts = prometheus.NewDesc(
		"mozia_redis_pool_timeouts_total",
		"Cumulative number of times a wait timeout occurred on the Redis pool.",
		nil, nil,
	)
	descBatchPending = prometheus.NewDesc(
		"mozia_batch_update_pending_items",
		"Number of unsaved billing/quota records currently buffered in the in-memory batch updater.",
		[]string{"type"}, nil,
	)
	descChannelsTotal = prometheus.NewDesc(
		"mozia_channels_total",
		"Total configured channels in memory cache grouped by status.",
		[]string{"status"}, nil,
	)
	descModelHealthyChannels = prometheus.NewDesc(
		"mozia_model_healthy_channels",
		"Number of currently enabled channels available for a given group and model.",
		[]string{"group", "model"}, nil,
	)

	defaultRecorder = NewRecorder()
)

// Default returns the global metrics recorder used by the running process.
func Default() *Recorder {
	return defaultRecorder
}

// NewRecorder builds an isolated Prometheus recorder (also usable in tests).
func NewRecorder() *Recorder {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)

	r := &Recorder{
		registry:     reg,
		vip:          newVIPConfigFromEnv(),
		modelLimiter: newLabelLimiter(parsePositiveIntEnv("METRICS_MAX_MODELS", defaultMaxTrackedModels)),
		groupLimiter: newLabelLimiter(parsePositiveIntEnv("METRICS_MAX_GROUPS", defaultMaxTrackedGroups)),
		buildInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "mozia_build_info",
			Help: "Build and node identity information for this mozia-api instance.",
		}, []string{"node_name", "role", "version"}),
		relayRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mozia_relay_requests_total",
			Help: "Total relay API requests classified by outcome, error class, and HTTP status class.",
		}, []string{"model", "group", "outcome", "error_class", "status_class"}),
		relayDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "mozia_relay_request_duration_seconds",
			Help:    "End-to-end relay request duration in seconds.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 40, 80, 160, 320},
		}, []string{"model", "group", "is_stream"}),
		channelAttempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mozia_relay_channel_attempts_total",
			Help: "Upstream channel attempts inside the relay retry loop.",
		}, []string{"channel_id", "channel_name", "model", "result", "is_retry"}),
		channelAutoDisable: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mozia_channel_auto_disable_total",
			Help: "Total channel auto-disable events triggered by upstream errors.",
		}, []string{"channel_id", "channel_name"}),
		ttftSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "mozia_relay_ttft_seconds",
			Help:    "Time to first token (TTFT) in seconds for streaming relay requests.",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 3, 5, 8, 12, 20, 30, 60},
		}, []string{"model", "group"}),
		tpsHistogram: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "mozia_relay_tps",
			Help:    "Output generation speed in completion tokens per second.",
			Buckets: []float64{2, 5, 10, 20, 35, 50, 75, 100, 150, 250, 500},
		}, []string{"model", "group"}),
		gatewayOverhead: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "mozia_relay_gateway_overhead_seconds",
			Help:    "Gateway pre-upstream processing overhead before first upstream HTTP call.",
			Buckets: []float64{0.002, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
		}, []string{"model", "group"}),
		streamOutcomes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mozia_relay_stream_outcomes_total",
			Help: "Terminal outcomes of streaming SSE relay responses.",
		}, []string{"model", "group", "status", "end_reason"}),
		tokensTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mozia_relay_tokens_total",
			Help: "Total prompt and completion tokens billed by model and group.",
		}, []string{"model", "group", "direction"}),
		quotaConsumed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mozia_relay_quota_consumed_total",
			Help: "Total customer quota units consumed by model and group.",
		}, []string{"model", "group"}),
		billingErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mozia_billing_errors_total",
			Help: "Billing failures grouped by stage and failure reason.",
		}, []string{"stage", "reason"}),
		paymentWebhooks: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mozia_payment_webhook_total",
			Help: "Payment webhook callbacks grouped by provider and handling status.",
		}, []string{"provider", "status"}),

		vipRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mozia_vip_requests_total",
			Help: "Total relay requests for watched key customers (VIP users/groups).",
		}, []string{"customer", "user_id", "model", "outcome", "error_class"}),
		vipTTFTSeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "mozia_vip_ttft_seconds",
			Help:    "Time to first token (TTFT) in seconds for watched key customers.",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 3, 5, 8, 12, 20, 30, 60},
		}, []string{"customer", "user_id", "model"}),
		vipTPSHistogram: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "mozia_vip_tps",
			Help:    "Output generation speed (tokens/sec) for watched key customers.",
			Buckets: []float64{2, 5, 10, 20, 35, 50, 75, 100, 150, 250, 500},
		}, []string{"customer", "user_id", "model"}),
		vipStreamOutcomes: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mozia_vip_stream_outcomes_total",
			Help: "Streaming SSE terminal outcomes for watched key customers.",
		}, []string{"customer", "user_id", "model", "status", "end_reason"}),
		vipTokensTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mozia_vip_tokens_total",
			Help: "Total prompt and completion tokens for watched key customers.",
		}, []string{"customer", "user_id", "model", "direction"}),
		vipQuotaConsumed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "mozia_vip_quota_consumed_total",
			Help: "Total customer quota units consumed by watched key customers.",
		}, []string{"customer", "user_id", "model"}),
	}

	r.ready.Store(1)

	reg.MustRegister(
		r.buildInfo,
		r.relayRequests,
		r.relayDuration,
		r.channelAttempts,
		r.channelAutoDisable,
		r.ttftSeconds,
		r.tpsHistogram,
		r.gatewayOverhead,
		r.streamOutcomes,
		r.tokensTotal,
		r.quotaConsumed,
		r.billingErrors,
		r.paymentWebhooks,
		r.vipRequests,
		r.vipTTFTSeconds,
		r.vipTPSHistogram,
		r.vipStreamOutcomes,
		r.vipTokensTotal,
		r.vipQuotaConsumed,
		&runtimeCollector{recorder: r},
	)

	return r
}

// Handler returns the HTTP handler serving Prometheus text exposition format.
func (r *Recorder) Handler() http.Handler {
	return promhttp.HandlerFor(r.registry, promhttp.HandlerOpts{})
}

// Registry exposes the underlying Prometheus registry for inspection and tests.
func (r *Recorder) Registry() *prometheus.Registry {
	return r.registry
}

// VIP returns the VIP watchlist configuration handle.
func (r *Recorder) VIP() *VIPConfig {
	return r.vip
}

// SetBuildInfo sets the static instance identity label once on startup.
func (r *Recorder) SetBuildInfo(nodeName, role, version string) {
	r.buildInfoOnce.Do(func() {
		if nodeName == "" {
			nodeName = "unknown"
		}
		if role == "" {
			role = "master"
		}
		if version == "" {
			version = "unknown"
		}
		r.buildInfo.WithLabelValues(nodeName, role, version).Set(1)
	})
}

// SetReady sets the instance readiness gauge (1=ready, 0=draining/unready).
func (r *Recorder) SetReady(ready bool) {
	if ready {
		r.ready.Store(1)
	} else {
		r.ready.Store(0)
	}
}

// SetRuntimeProviders registers scrape-time providers for DB, Redis, active
// connections, batch update queue, and channel cache snapshots.
func (r *Recorder) SetRuntimeProviders(p RuntimeProviders) {
	r.providersMu.Lock()
	defer r.providersMu.Unlock()
	r.providers = p
}

func (r *Recorder) getRuntimeProviders() RuntimeProviders {
	r.providersMu.RLock()
	defer r.providersMu.RUnlock()
	return r.providers
}

// RecordRequestOutcome increments relay request counters, duration histogram,
// and matching VIP request metrics.
func (r *Recorder) RecordRequestOutcome(s RequestOutcomeSample) {
	modelLabel := r.modelLimiter.Normalize(s.Model, "unknown")
	groupLabel := r.groupLimiter.Normalize(s.Group, "default")
	outcome := normalizeSimpleEnum(s.Outcome, "unknown")
	errClass := normalizeSimpleEnum(s.ErrorClass, "none")
	statusClass := httpStatusClass(s.StatusCode)
	isStream := "false"
	if s.IsStream {
		isStream = "true"
	}

	r.relayRequests.WithLabelValues(modelLabel, groupLabel, outcome, errClass, statusClass).Inc()
	if s.Duration >= 0 {
		r.relayDuration.WithLabelValues(modelLabel, groupLabel, isStream).Observe(s.Duration.Seconds())
	}

	if customer, userIDLabel, ok := r.vip.Resolve(s.UserID, s.Username, s.Group); ok {
		r.vipRequests.WithLabelValues(customer, userIDLabel, modelLabel, outcome, errClass).Inc()
	}
}

// RecordChannelAttempt records an individual upstream channel attempt inside
// the relay retry loop to distinguish retry rate from final user failure rate.
func (r *Recorder) RecordChannelAttempt(channelID int, channelName, model, result string, isRetry bool) {
	modelLabel := r.modelLimiter.Normalize(model, "unknown")
	nameLabel := sanitizeLabelValue(channelName, 48)
	if nameLabel == "" {
		nameLabel = "unnamed"
	}
	retryLabel := "false"
	if isRetry {
		retryLabel = "true"
	}
	r.channelAttempts.WithLabelValues(
		formatChannelID(channelID),
		nameLabel,
		modelLabel,
		normalizeSimpleEnum(result, "unknown"),
		retryLabel,
	).Inc()
}

// RecordChannelAutoDisable increments the auto-disable counter when a channel
// is automatically banned due to upstream errors.
func (r *Recorder) RecordChannelAutoDisable(channelID int, channelName string) {
	nameLabel := sanitizeLabelValue(channelName, 48)
	if nameLabel == "" {
		nameLabel = "unnamed"
	}
	r.channelAutoDisable.WithLabelValues(formatChannelID(channelID), nameLabel).Inc()
}

// RecordGatewayOverhead records the gateway's own pre-upstream latency
// (authentication, token estimation, pricing, channel selection) before sending
// the first HTTP request upstream.
func (r *Recorder) RecordGatewayOverhead(model, group string, d time.Duration) {
	if d < 0 {
		return
	}
	modelLabel := r.modelLimiter.Normalize(model, "unknown")
	groupLabel := r.groupLimiter.Normalize(group, "default")
	r.gatewayOverhead.WithLabelValues(modelLabel, groupLabel).Observe(d.Seconds())
}

// RecordRelayPerf records TTFT, TPS, token counts, and quota consumption for a
// completed relay request (and mirrors to VIP metrics when matched).
func (r *Recorder) RecordRelayPerf(s RelayPerfSample) {
	modelLabel := r.modelLimiter.Normalize(s.Model, "unknown")
	groupLabel := r.groupLimiter.Normalize(s.Group, "default")

	customer, userIDLabel, isVIP := r.vip.Resolve(s.UserID, s.Username, s.Group)

	if s.HasTTFT && s.TTFT >= 0 {
		ttftSec := s.TTFT.Seconds()
		r.ttftSeconds.WithLabelValues(modelLabel, groupLabel).Observe(ttftSec)
		if isVIP {
			r.vipTTFTSeconds.WithLabelValues(customer, userIDLabel, modelLabel).Observe(ttftSec)
		}
	}

	if s.CompletionTokens > 0 && s.GenerationTime > 0 {
		tps := float64(s.CompletionTokens) / s.GenerationTime.Seconds()
		if tps > 0 {
			r.tpsHistogram.WithLabelValues(modelLabel, groupLabel).Observe(tps)
			if isVIP {
				r.vipTPSHistogram.WithLabelValues(customer, userIDLabel, modelLabel).Observe(tps)
			}
		}
	}

	if s.PromptTokens > 0 {
		r.tokensTotal.WithLabelValues(modelLabel, groupLabel, "prompt").Add(float64(s.PromptTokens))
		if isVIP {
			r.vipTokensTotal.WithLabelValues(customer, userIDLabel, modelLabel, "prompt").Add(float64(s.PromptTokens))
		}
	}
	if s.CompletionTokens > 0 {
		r.tokensTotal.WithLabelValues(modelLabel, groupLabel, "completion").Add(float64(s.CompletionTokens))
		if isVIP {
			r.vipTokensTotal.WithLabelValues(customer, userIDLabel, modelLabel, "completion").Add(float64(s.CompletionTokens))
		}
	}
	if s.QuotaConsumed > 0 {
		r.quotaConsumed.WithLabelValues(modelLabel, groupLabel).Add(float64(s.QuotaConsumed))
		if isVIP {
			r.vipQuotaConsumed.WithLabelValues(customer, userIDLabel, modelLabel).Add(float64(s.QuotaConsumed))
		}
	}
}

// RecordStreamOutcome records whether an SSE stream ended cleanly, was
// abnormally interrupted mid-stream, or was disconnected by the client.
func (r *Recorder) RecordStreamOutcome(s StreamOutcomeSample) {
	modelLabel := r.modelLimiter.Normalize(s.Model, "unknown")
	groupLabel := r.groupLimiter.Normalize(s.Group, "default")
	status := normalizeSimpleEnum(s.Status, "unknown")
	endReason := normalizeSimpleEnum(s.EndReason, "unknown")

	r.streamOutcomes.WithLabelValues(modelLabel, groupLabel, status, endReason).Inc()
	if customer, userIDLabel, ok := r.vip.Resolve(s.UserID, s.Username, s.Group); ok {
		r.vipStreamOutcomes.WithLabelValues(customer, userIDLabel, modelLabel, status, endReason).Inc()
	}
}

// RecordBillingError records a billing pre-consume, settlement, or refund error.
func (r *Recorder) RecordBillingError(stage, reason string) {
	r.billingErrors.WithLabelValues(
		normalizeSimpleEnum(stage, "unknown"),
		normalizeSimpleEnum(reason, "unknown"),
	).Inc()
}

// RecordPaymentWebhook records payment callback outcomes by provider.
func (r *Recorder) RecordPaymentWebhook(provider, status string) {
	r.paymentWebhooks.WithLabelValues(
		normalizeSimpleEnum(provider, "unknown"),
		normalizeSimpleEnum(status, "unknown"),
	).Inc()
}

// Package-level convenience wrappers delegating to Default().
func SetBuildInfo(nodeName, role, version string) {
	defaultRecorder.SetBuildInfo(nodeName, role, version)
}

func SetReady(ready bool) {
	defaultRecorder.SetReady(ready)
}

func SetRuntimeProviders(p RuntimeProviders) {
	defaultRecorder.SetRuntimeProviders(p)
}

func SetDynamicVIPUsers(spec string) {
	defaultRecorder.VIP().SetDynamicUsers(spec)
}

func SetDynamicVIPGroups(spec string) {
	defaultRecorder.VIP().SetDynamicGroups(spec)
}

func RecordRequestOutcome(s RequestOutcomeSample) {
	defaultRecorder.RecordRequestOutcome(s)
}

func RecordChannelAttempt(channelID int, channelName, model, result string, isRetry bool) {
	defaultRecorder.RecordChannelAttempt(channelID, channelName, model, result, isRetry)
}

func RecordChannelAutoDisable(channelID int, channelName string) {
	defaultRecorder.RecordChannelAutoDisable(channelID, channelName)
}

func RecordGatewayOverhead(model, group string, d time.Duration) {
	defaultRecorder.RecordGatewayOverhead(model, group, d)
}

func RecordRelayPerf(s RelayPerfSample) {
	defaultRecorder.RecordRelayPerf(s)
}

func RecordStreamOutcome(s StreamOutcomeSample) {
	defaultRecorder.RecordStreamOutcome(s)
}

func RecordBillingError(stage, reason string) {
	defaultRecorder.RecordBillingError(stage, reason)
}

func RecordPaymentWebhook(provider, status string) {
	defaultRecorder.RecordPaymentWebhook(provider, status)
}

func httpStatusClass(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return "other"
	}
}

func normalizeSimpleEnum(val, fallback string) string {
	val = strings.ToLower(strings.TrimSpace(val))
	if val == "" {
		return fallback
	}
	return sanitizeLabelValue(val, 48)
}

// runtimeCollector exports scrape-time gauges from live process state.
type runtimeCollector struct {
	recorder *Recorder
}

func (c *runtimeCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- descInstanceReady
	ch <- descActiveConns
	ch <- descDBPoolConns
	ch <- descDBPoolWaitCount
	ch <- descDBPoolWaitSeconds
	ch <- descRedisPoolConns
	ch <- descRedisPoolTimeouts
	ch <- descBatchPending
	ch <- descChannelsTotal
	ch <- descModelHealthyChannels
}

func (c *runtimeCollector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(descInstanceReady, prometheus.GaugeValue, float64(c.recorder.ready.Load()))

	p := c.recorder.getRuntimeProviders()

	if p.ActiveConnections != nil {
		ch <- prometheus.MustNewConstMetric(descActiveConns, prometheus.GaugeValue, float64(p.ActiveConnections()))
	}

	if p.DBPoolStats != nil {
		for dbName, stats := range p.DBPoolStats() {
			ch <- prometheus.MustNewConstMetric(descDBPoolConns, prometheus.GaugeValue, float64(stats.MaxOpenConnections), dbName, "max_open")
			ch <- prometheus.MustNewConstMetric(descDBPoolConns, prometheus.GaugeValue, float64(stats.OpenConnections), dbName, "open")
			ch <- prometheus.MustNewConstMetric(descDBPoolConns, prometheus.GaugeValue, float64(stats.InUse), dbName, "in_use")
			ch <- prometheus.MustNewConstMetric(descDBPoolConns, prometheus.GaugeValue, float64(stats.Idle), dbName, "idle")
			ch <- prometheus.MustNewConstMetric(descDBPoolWaitCount, prometheus.CounterValue, float64(stats.WaitCount), dbName)
			ch <- prometheus.MustNewConstMetric(descDBPoolWaitSeconds, prometheus.CounterValue, stats.WaitDuration.Seconds(), dbName)
		}
	}

	if p.RedisPoolStats != nil {
		if stats, ok := p.RedisPoolStats(); ok {
			ch <- prometheus.MustNewConstMetric(descRedisPoolConns, prometheus.GaugeValue, float64(stats.TotalConns), "total")
			ch <- prometheus.MustNewConstMetric(descRedisPoolConns, prometheus.GaugeValue, float64(stats.IdleConns), "idle")
			ch <- prometheus.MustNewConstMetric(descRedisPoolConns, prometheus.GaugeValue, float64(stats.StaleConns), "stale")
			ch <- prometheus.MustNewConstMetric(descRedisPoolTimeouts, prometheus.CounterValue, float64(stats.Timeouts))
		}
	}

	if p.BatchUpdatePending != nil {
		for batchType, count := range p.BatchUpdatePending() {
			ch <- prometheus.MustNewConstMetric(descBatchPending, prometheus.GaugeValue, float64(count), batchType)
		}
	}

	if p.ChannelCacheSummary != nil {
		if snap, ok := p.ChannelCacheSummary(); ok {
			for status, count := range snap.StatusCounts {
				ch <- prometheus.MustNewConstMetric(descChannelsTotal, prometheus.GaugeValue, float64(count), status)
			}
			emitted := 0
			const maxModelChannelSeries = 2000
			for group, models := range snap.ModelHealthyChannel {
				groupLabel := c.recorder.groupLimiter.Normalize(group, "default")
				if groupLabel == overflowLabelValue {
					continue
				}
				for modelName, count := range models {
					if emitted >= maxModelChannelSeries {
						break
					}
					modelLabel := c.recorder.modelLimiter.Normalize(modelName, "unknown")
					if modelLabel == overflowLabelValue {
						continue
					}
					ch <- prometheus.MustNewConstMetric(descModelHealthyChannels, prometheus.GaugeValue, float64(count), groupLabel, modelLabel)
					emitted++
				}
			}
		}
	}
}
