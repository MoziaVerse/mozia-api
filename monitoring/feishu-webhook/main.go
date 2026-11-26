package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Config struct {
	ListenAddr         string
	DefaultWebhookURL  string
	DefaultSecret      string
	CriticalWebhookURL string
	CriticalSecret     string
	GrafanaURL         string
	AlertmanagerURL    string
	UptimeKumaURL      string
	EnvLabel           string
	AtAllOnCritical    bool
	HTTPTimeout        time.Duration
}

type AlertmanagerPayload struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	Status            string            `json:"status"`
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []Alert           `json:"alerts"`
}

type Alert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

type FeishuCardRequest struct {
	Timestamp string     `json:"timestamp,omitempty"`
	Sign      string     `json:"sign,omitempty"`
	MsgType   string     `json:"msg_type"`
	Card      FeishuCard `json:"card"`
}

type FeishuCard struct {
	Config   FeishuCardConfig `json:"config"`
	Header   FeishuCardHeader `json:"header"`
	Elements []any            `json:"elements"`
}

type FeishuCardConfig struct {
	WideScreenMode bool `json:"wide_screen_mode"`
}

type FeishuCardHeader struct {
	Template string           `json:"template"`
	Title    FeishuPlainText  `json:"title"`
}

type FeishuPlainText struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

type FeishuMarkdownElement struct {
	Tag     string `json:"tag"`
	Content string `json:"content"`
}

type FeishuResponse struct {
	StatusCode int    `json:"StatusCode"`
	StatusMsg  string `json:"StatusMessage"`
	Code       int    `json:"code"`
	Msg        string `json:"msg"`
}

var shanghaiLoc = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}()

func loadConfigFromEnv() Config {
	listenAddr := strings.TrimSpace(os.Getenv("LISTEN_ADDR"))
	if listenAddr == "" {
		listenAddr = ":8060"
	}
	envLabel := strings.TrimSpace(os.Getenv("ALERT_ENV"))
	if envLabel == "" {
		envLabel = "mozia-api"
	}
	return Config{
		ListenAddr:         listenAddr,
		DefaultWebhookURL:  strings.TrimSpace(os.Getenv("FEISHU_WEBHOOK_URL")),
		DefaultSecret:      strings.TrimSpace(os.Getenv("FEISHU_SECRET")),
		CriticalWebhookURL: strings.TrimSpace(os.Getenv("FEISHU_CRITICAL_WEBHOOK_URL")),
		CriticalSecret:     strings.TrimSpace(os.Getenv("FEISHU_CRITICAL_SECRET")),
		GrafanaURL:         strings.TrimRight(strings.TrimSpace(os.Getenv("GRAFANA_EXTERNAL_URL")), "/"),
		AlertmanagerURL:    strings.TrimRight(strings.TrimSpace(os.Getenv("ALERTMANAGER_EXTERNAL_URL")), "/"),
		UptimeKumaURL:      strings.TrimRight(strings.TrimSpace(os.Getenv("UPTIME_KUMA_EXTERNAL_URL")), "/"),
		EnvLabel:           envLabel,
		AtAllOnCritical:    strings.EqualFold(strings.TrimSpace(os.Getenv("FEISHU_AT_ALL_ON_CRITICAL")), "true"),
		HTTPTimeout:        10 * time.Second,
	}
}

// ComputeFeishuSign implements Feishu custom bot webhook v2 signature:
// base64(HMAC-SHA256(key = "<timestamp>\n<secret>", msg = "")).
func ComputeFeishuSign(secret string, timestamp int64) string {
	stringToSign := strconv.FormatInt(timestamp, 10) + "\n" + secret
	mac := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

type RelayServer struct {
	cfg    Config
	client *http.Client
	nowFn  func() time.Time
}

func NewRelayServer(cfg Config) *RelayServer {
	timeout := cfg.HTTPTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &RelayServer{
		cfg:    cfg,
		client: &http.Client{Timeout: timeout},
		nowFn:  time.Now,
	}
}

func (s *RelayServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/webhook", s.handleWebhook)
	mux.HandleFunc("/webhook/", s.handleWebhook)
	return mux
}

func (s *RelayServer) resolveTarget(routePath string, payload AlertmanagerPayload) (string, string) {
	isCriticalRoute := strings.HasSuffix(strings.TrimRight(routePath, "/"), "/critical")
	severity := strings.ToLower(payload.CommonLabels["severity"])
	if (isCriticalRoute || severity == "critical") && s.cfg.CriticalWebhookURL != "" {
		secret := s.cfg.CriticalSecret
		if secret == "" {
			secret = s.cfg.DefaultSecret
		}
		return s.cfg.CriticalWebhookURL, secret
	}
	return s.cfg.DefaultWebhookURL, s.cfg.DefaultSecret
}

func (s *RelayServer) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()

	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var payload AlertmanagerPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid alertmanager json: "+err.Error(), http.StatusBadRequest)
		return
	}

	targetURL, secret := s.resolveTarget(r.URL.Path, payload)
	if targetURL == "" {
		log.Printf("skipping alert delivery: FEISHU_WEBHOOK_URL is not configured")
		http.Error(w, "feishu webhook url not configured", http.StatusServiceUnavailable)
		return
	}

	card := s.BuildCard(payload)
	if err := s.SendCard(r.Context(), targetURL, secret, card); err != nil {
		log.Printf("failed to deliver alert to Feishu: %v", err)
		http.Error(w, "feishu delivery failed: "+err.Error(), http.StatusBadGateway)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (s *RelayServer) BuildCard(payload AlertmanagerPayload) FeishuCard {
	status := strings.ToLower(strings.TrimSpace(payload.Status))
	severity := strings.ToLower(strings.TrimSpace(payload.CommonLabels["severity"]))
	if severity == "" && len(payload.Alerts) > 0 {
		severity = strings.ToLower(strings.TrimSpace(payload.Alerts[0].Labels["severity"]))
	}
	alertName := payload.CommonLabels["alertname"]
	if alertName == "" {
		alertName = payload.GroupLabels["alertname"]
	}
	if alertName == "" {
		alertName = "Mozia 监控告警"
	}

	template := "blue"
	prefix := "通知"
	switch {
	case status == "resolved":
		template = "green"
		prefix = "已恢复"
	case severity == "critical":
		template = "red"
		prefix = "紧急告警"
	case severity == "warning":
		template = "orange"
		prefix = "警告"
	}

	title := fmt.Sprintf("[%s][%s] %s (%d条)", prefix, s.cfg.EnvLabel, alertName, len(payload.Alerts))

	var b strings.Builder
	if status != "resolved" && severity == "critical" && s.cfg.AtAllOnCritical {
		b.WriteString("<at id=all></at>\n")
	}

	const maxShown = 8
	for i, alert := range payload.Alerts {
		if i >= maxShown {
			fmt.Fprintf(&b, "\n... 其余 %d 条告警已折叠，请打开 Grafana 查看详情。", len(payload.Alerts)-maxShown)
			break
		}
		if i > 0 {
			b.WriteString("\n---\n")
		}
		name := firstNonEmpty(alert.Labels["alertname"], alertName)
		sev := firstNonEmpty(alert.Labels["severity"], severity, "info")
		inst := firstNonEmpty(alert.Labels["node_name"], alert.Labels["instance"], alert.Labels["name"], "-")
		summary := firstNonEmpty(alert.Annotations["summary"], name)
		desc := strings.TrimSpace(alert.Annotations["description"])

		fmt.Fprintf(&b, "**%d. %s** (`%s`)\n", i+1, summary, sev)
		fmt.Fprintf(&b, "- **规则**: `%s`  |  **目标**: `%s`\n", name, inst)

		if customer := alert.Labels["customer"]; customer != "" {
			fmt.Fprintf(&b, "- **大客户**: `%s` (ID: `%s`)\n", customer, alert.Labels["user_id"])
		}
		if model := alert.Labels["model"]; model != "" {
			fmt.Fprintf(&b, "- **模型**: `%s`\n", model)
		}
		if desc != "" {
			fmt.Fprintf(&b, "- **详情**: %s\n", desc)
		}
		if !alert.StartsAt.IsZero() {
			fmt.Fprintf(&b, "- **触发时间**: %s\n", alert.StartsAt.In(shanghaiLoc).Format("2006-01-02 15:04:05"))
		}
		if strings.EqualFold(alert.Status, "resolved") && !alert.EndsAt.IsZero() {
			fmt.Fprintf(&b, "- **恢复时间**: %s\n", alert.EndsAt.In(shanghaiLoc).Format("2006-01-02 15:04:05"))
		}
	}

	if len(payload.Alerts) == 0 {
		b.WriteString("收到空告警列表（心跳/测试事件）。\n")
	}

	elements := []any{
		FeishuMarkdownElement{
			Tag:     "markdown",
			Content: strings.TrimSpace(b.String()),
		},
	}

	var links []string
	if u := strings.TrimRight(strings.TrimSpace(s.cfg.GrafanaURL), "/"); u != "" {
		links = append(links, fmt.Sprintf("[打开 Grafana 大盘](%s)", u))
	}
	if u := strings.TrimRight(strings.TrimSpace(s.cfg.UptimeKumaURL), "/"); u != "" {
		links = append(links, fmt.Sprintf("[打开 Uptime Kuma](%s)", u))
	}
	amURL := strings.TrimRight(strings.TrimSpace(s.cfg.AlertmanagerURL), "/")
	if amURL == "" && isRoutableExternalURL(payload.ExternalURL) {
		amURL = strings.TrimRight(strings.TrimSpace(payload.ExternalURL), "/")
	}
	if amURL != "" {
		links = append(links, fmt.Sprintf("[打开 Alertmanager](%s)", amURL))
	}
	if len(links) > 0 {
		sort.Strings(links)
		elements = append(elements, FeishuMarkdownElement{
			Tag:     "markdown",
			Content: strings.Join(links, "  |  "),
		})
	}

	return FeishuCard{
		Config: FeishuCardConfig{WideScreenMode: true},
		Header: FeishuCardHeader{
			Template: template,
			Title: FeishuPlainText{
				Tag:     "plain_text",
				Content: title,
			},
		},
		Elements: elements,
	}
}

func (s *RelayServer) SendCard(ctx context.Context, webhookURL, secret string, card FeishuCard) error {
	reqBody := FeishuCardRequest{
		MsgType: "interactive",
		Card:    card,
	}
	if secret != "" {
		ts := s.nowFn().Unix()
		reqBody.Timestamp = strconv.FormatInt(ts, 10)
		reqBody.Sign = ComputeFeishuSign(secret, ts)
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(reqBody); err != nil {
		return err
	}
	raw := buf.Bytes()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("feishu http status %d: %s", resp.StatusCode, string(respBytes))
	}

	var parsed FeishuResponse
	if err := json.Unmarshal(respBytes, &parsed); err == nil {
		if parsed.StatusCode != 0 {
			return fmt.Errorf("feishu error StatusCode=%d: %s", parsed.StatusCode, parsed.StatusMsg)
		}
		if parsed.Code != 0 {
			return fmt.Errorf("feishu error code=%d: %s", parsed.Code, parsed.Msg)
		}
	}
	return nil
}

func isRoutableExternalURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return false
	}
	// Reject bare Docker container hostnames/IDs such as "c4aa4e26fefe" or "alertmanager".
	return strings.Contains(host, ".")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func main() {
	sendTest := flag.Bool("send-test", false, "send a single verification card to FEISHU_WEBHOOK_URL and exit")
	flag.Parse()

	cfg := loadConfigFromEnv()
	relay := NewRelayServer(cfg)

	if *sendTest {
		if cfg.DefaultWebhookURL == "" {
			log.Fatal("FEISHU_WEBHOOK_URL is required for --send-test")
		}
		now := time.Now()
		payload := AlertmanagerPayload{
			Status: "firing",
			CommonLabels: map[string]string{
				"alertname": "MoziaMonitoringSelfTest",
				"severity":  "warning",
			},
			Alerts: []Alert{
				{
					Status: "firing",
					Labels: map[string]string{
						"alertname": "MoziaMonitoringSelfTest",
						"severity":  "warning",
						"node_name": cfg.EnvLabel,
					},
					Annotations: map[string]string{
						"summary":     "Mozia 监控告警通道连通性测试",
						"description": "如果你在飞书群里看到这条带签名校验的卡片消息，说明 Alertmanager -> Feishu Webhook 告警链路已打通。",
					},
					StartsAt: now,
				},
			},
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := relay.SendCard(ctx, cfg.DefaultWebhookURL, cfg.DefaultSecret, relay.BuildCard(payload)); err != nil {
			log.Fatalf("test alert failed: %v", err)
		}
		log.Println("test alert delivered successfully to Feishu")
		return
	}

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           relay.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("mozia-feishu-webhook listening on %s (env=%s)", cfg.ListenAddr, cfg.EnvLabel)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		if err != nil {
			log.Fatalf("server error: %v", err)
		}
	case <-sigCtx.Done():
		log.Println("shutting down mozia-feishu-webhook")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
}
