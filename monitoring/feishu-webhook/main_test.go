package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestComputeFeishuSign(t *testing.T) {
	// Verify deterministic HMAC-SHA256 output for Feishu v2 signing contract.
	sign := ComputeFeishuSign("demo-secret", 1700000000)
	if sign == "" {
		t.Fatal("expected non-empty signature")
	}
	if sign != ComputeFeishuSign("demo-secret", 1700000000) {
		t.Fatal("signature must be deterministic")
	}
	if sign == ComputeFeishuSign("other-secret", 1700000000) {
		t.Fatal("different secrets must produce different signatures")
	}
}

func TestWebhookForwardsSignedCardAndRoutesCritical(t *testing.T) {
	var defaultReceived, criticalReceived FeishuCardRequest

	defaultMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&defaultReceived); err != nil {
			t.Errorf("decode default body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"StatusCode":0,"StatusMessage":"success"}`))
	}))
	defer defaultMock.Close()

	criticalMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&criticalReceived); err != nil {
			t.Errorf("decode critical body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer criticalMock.Close()

	relay := NewRelayServer(Config{
		DefaultWebhookURL:  defaultMock.URL,
		DefaultSecret:      "sec-default",
		CriticalWebhookURL: criticalMock.URL,
		CriticalSecret:     "sec-critical",
		EnvLabel:           "test-21",
		AtAllOnCritical:    true,
	})
	fixedTime := time.Unix(1700000123, 0)
	relay.nowFn = func() time.Time { return fixedTime }

	// 1) Warning alert -> default webhook with orange header
	warnPayload := AlertmanagerPayload{
		Status: "firing",
		CommonLabels: map[string]string{
			"alertname": "MoziaHighTTFT",
			"severity":  "warning",
		},
		Alerts: []Alert{
			{
				Status: "firing",
				Labels: map[string]string{
					"alertname": "MoziaHighTTFT",
					"severity":  "warning",
					"model":     "gpt-4o",
					"customer":  "VIP-6218",
					"user_id":   "6218",
				},
				Annotations: map[string]string{
					"summary":     "大客户 TTFT P95 过高",
					"description": "当前 P95 为 6.2s",
				},
				StartsAt: fixedTime,
			},
		},
	}
	rawWarn, _ := json.Marshal(warnPayload)
	rec := httptest.NewRecorder()
	relay.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(rawWarn)))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if defaultReceived.Sign != ComputeFeishuSign("sec-default", fixedTime.Unix()) {
		t.Fatalf("unexpected default signature: %s", defaultReceived.Sign)
	}
	if defaultReceived.Card.Header.Template != "orange" {
		t.Fatalf("expected orange header for warning, got %s", defaultReceived.Card.Header.Template)
	}

	// 2) Critical alert -> critical webhook with red header and @all mention
	critPayload := AlertmanagerPayload{
		Status: "firing",
		CommonLabels: map[string]string{
			"alertname": "MoziaInstanceDown",
			"severity":  "critical",
		},
		Alerts: []Alert{
			{
				Status: "firing",
				Labels: map[string]string{
					"alertname": "MoziaInstanceDown",
					"severity":  "critical",
					"node_name": "mozia-api-primary",
				},
				Annotations: map[string]string{
					"summary": "实例宕机",
				},
				StartsAt: fixedTime,
			},
		},
	}
	rawCrit, _ := json.Marshal(critPayload)
	recCrit := httptest.NewRecorder()
	relay.Handler().ServeHTTP(recCrit, httptest.NewRequest(http.MethodPost, "/webhook/critical", bytes.NewReader(rawCrit)))
	if recCrit.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", recCrit.Code, recCrit.Body.String())
	}
	if criticalReceived.Sign != ComputeFeishuSign("sec-critical", fixedTime.Unix()) {
		t.Fatalf("unexpected critical signature: %s", criticalReceived.Sign)
	}
	if criticalReceived.Card.Header.Template != "red" {
		t.Fatalf("expected red header for critical, got %s", criticalReceived.Card.Header.Template)
	}
	var mdBuf bytes.Buffer
	enc := json.NewEncoder(&mdBuf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(criticalReceived.Card.Elements)
	if !strings.Contains(mdBuf.String(), "<at id=all></at>") {
		t.Fatalf("expected @all mention in critical card, got %s", mdBuf.String())
	}
}

func TestWebhookReturns502WhenFeishuRejects(t *testing.T) {
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":19021,"msg":"sign match fail"}`))
	}))
	defer mock.Close()

	relay := NewRelayServer(Config{
		DefaultWebhookURL: mock.URL,
		DefaultSecret:     "wrong",
	})
	rec := httptest.NewRecorder()
	relay.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(`{"status":"firing","alerts":[]}`)))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on Feishu rejection, got %d", rec.Code)
	}
}

func TestBuildCardFiltersUnroutableAlertmanagerContainerURL(t *testing.T) {
	relay := NewRelayServer(Config{
		GrafanaURL:    "https://grafana.mzsjai.com",
		UptimeKumaURL: "https://kuma.mzsjai.com",
		EnvLabel:      "prod",
	})

	card := relay.BuildCard(AlertmanagerPayload{
		Status:      "firing",
		ExternalURL: "http://c4aa4e26fefe:9093",
		CommonLabels: map[string]string{
			"alertname": "MoziaHighGateway5xxRate",
			"severity":  "critical",
		},
	})
	raw, _ := json.Marshal(card)
	body := string(raw)
	if strings.Contains(body, "c4aa4e26fefe") || strings.Contains(body, "Alertmanager") {
		t.Fatalf("expected internal container URL to be omitted, got: %s", body)
	}
	if !strings.Contains(body, "https://grafana.mzsjai.com") || !strings.Contains(body, "https://kuma.mzsjai.com") {
		t.Fatalf("expected Grafana and Uptime Kuma links, got: %s", body)
	}

	relayWithAM := NewRelayServer(Config{
		AlertmanagerURL: "https://alerts.mzsjai.com/",
		EnvLabel:        "prod",
	})
	cardWithAM := relayWithAM.BuildCard(AlertmanagerPayload{
		Status:      "firing",
		ExternalURL: "http://c4aa4e26fefe:9093",
	})
	rawAM, _ := json.Marshal(cardWithAM)
	if !strings.Contains(string(rawAM), "[打开 Alertmanager](https://alerts.mzsjai.com)") {
		t.Fatalf("expected explicit Alertmanager URL, got: %s", string(rawAM))
	}
}
