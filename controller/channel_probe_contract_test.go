package controller

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type probeAPIResponse struct {
	Success bool                       `json:"success"`
	Data    dto.ChannelBalanceProbeDTO `json:"data"`
}

type discoveryAPIResponse struct {
	Success bool                      `json:"success"`
	Data    []dto.ChannelDiscoveryDTO `json:"data"`
}

func TestGetChannelDiscoveryReturnsSanitizedProjection(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	baseURL := "https://API.Example.com:8443/v1/internal?token=secret"
	weight := uint(1)
	require.NoError(t, db.Create(&model.Channel{
		Id:      11,
		Name:    "discoverable",
		Type:    constant.ChannelTypeOpenAI,
		Key:     "top-secret-key",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &baseURL,
		Group:   "default",
		Models:  "gpt-4o",
		Weight:  &weight,
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/discovery", nil)

	GetChannelDiscovery(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	body := recorder.Body.String()
	require.NotContains(t, body, "top-secret-key")
	require.NotContains(t, body, `"key"`)
	require.NotContains(t, body, "/v1/internal")
	require.NotContains(t, body, "token=secret")

	var payload discoveryAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Len(t, payload.Data, 1)
	require.Equal(t, 11, payload.Data[0].ChannelID)
	require.Equal(t, "discoverable", payload.Data[0].ChannelName)
	require.Equal(t, constant.ChannelTypeOpenAI, payload.Data[0].ChannelType)
	require.Equal(t, "openai", payload.Data[0].ProviderIdentity)
	require.Equal(t, "https://api.example.com:8443", payload.Data[0].BaseURLIdentity)
	require.True(t, payload.Data[0].Enabled)
	require.True(t, payload.Data[0].ProbeCapability)
}

func TestProbeChannelBalanceReturnsUnsupportedTaxonomy(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.Create(&model.Channel{
		Id:     21,
		Name:   "azure-unsupported",
		Type:   constant.ChannelTypeAzure,
		Key:    "do-not-leak",
		Status: common.ChannelStatusEnabled,
		Group:  "default",
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "21"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/probe_balance/21", nil)

	ProbeChannelBalance(ctx)

	var payload probeAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Equal(t, "unsupported", payload.Data.Status)
	require.False(t, payload.Data.Supported)
	require.Nil(t, payload.Data.Balance)
	require.Equal(t, "unsupported_channel_type", payload.Data.SanitizedErrorCode)
	require.NotEmpty(t, payload.Data.SanitizedErrorMessage)
	require.NotContains(t, recorder.Body.String(), "do-not-leak")
}

func TestProbeChannelBalanceFailureSanitizesUpstreamErrors(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom secret upstream payload", http.StatusBadGateway)
	}))
	defer server.Close()

	baseURL := server.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:      22,
		Name:    "failing-openai",
		Type:    constant.ChannelTypeOpenAI,
		Key:     "top-secret-key",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &baseURL,
		Group:   "default",
		Models:  "gpt-4o",
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "22"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/probe_balance/22", nil)

	ProbeChannelBalance(ctx)

	var payload probeAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Equal(t, "failed", payload.Data.Status)
	require.True(t, payload.Data.Supported)
	require.Nil(t, payload.Data.Balance)
	require.Equal(t, "upstream_http_status", payload.Data.SanitizedErrorCode)
	require.Equal(t, "Upstream probe request failed", payload.Data.SanitizedErrorMessage)
	require.NotContains(t, recorder.Body.String(), "boom secret upstream payload")
	require.NotContains(t, recorder.Body.String(), "top-secret-key")
}

func TestProbeChannelBalanceSuccessDoesNotMutateChannel(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/dashboard/billing/subscription":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"hard_limit_usd":10,"has_payment_method":true}`))
		case "/v1/dashboard/billing/usage":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"total_usage":1000}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	baseURL := server.URL
	require.NoError(t, db.Create(&model.Channel{
		Id:                 23,
		Name:               "probe-target",
		Type:               constant.ChannelTypeOpenAI,
		Key:                "secret",
		Status:             common.ChannelStatusEnabled,
		BaseURL:            &baseURL,
		Balance:            7,
		BalanceUpdatedTime: 123,
		Group:              "default",
		Models:             "gpt-4o",
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "23"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/probe_balance/23", nil)

	ProbeChannelBalance(ctx)

	var payload probeAPIResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	require.Equal(t, "success", payload.Data.Status)
	require.True(t, payload.Data.Supported)
	require.NotNil(t, payload.Data.Balance)
	require.Equal(t, 0.0, *payload.Data.Balance)
	require.Equal(t, "USD", payload.Data.UnitOrCurrency)
	require.Empty(t, payload.Data.SanitizedErrorCode)
	require.Empty(t, payload.Data.SanitizedErrorMessage)

	var persisted model.Channel
	require.NoError(t, db.First(&persisted, "id = ?", 23).Error)
	require.Equal(t, common.ChannelStatusEnabled, persisted.Status)
	require.Equal(t, 7.0, persisted.Balance)
	require.Equal(t, int64(123), persisted.BalanceUpdatedTime)
}

type trackingReadCloser struct {
	io.Reader
	closed bool
}

func (t *trackingReadCloser) Close() error {
	t.closed = true
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestReadOnlyProbeBodyClosesResponseBodyOnNon200(t *testing.T) {
	body := &trackingReadCloser{Reader: strings.NewReader("upstream failure body")}
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Body:       body,
				Header:     make(http.Header),
			}, nil
		}),
	}

	_, err := readOnlyProbeBody(client, http.MethodGet, "https://example.com/probe", nil)

	require.Error(t, err)
	require.True(t, body.closed)
	var perr *probeError
	require.ErrorAs(t, err, &perr)
	require.Equal(t, "upstream_http_status", perr.Code)
}
