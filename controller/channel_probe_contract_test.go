package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestProbeChannelBalanceDoesNotPersistOrDisableChannel(t *testing.T) {
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
	channel := &model.Channel{
		Id:                 1,
		Name:               "probe-target",
		Type:               constant.ChannelTypeOpenAI,
		Key:                "secret",
		Status:             common.ChannelStatusEnabled,
		BaseURL:            &baseURL,
		Balance:            7,
		BalanceUpdatedTime: 123,
		Group:              "default",
		Models:             "gpt-4o",
	}
	require.NoError(t, db.Create(channel).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "1"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/probe_balance/1", nil)

	ProbeChannelBalance(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)
	require.Contains(t, recorder.Body.String(), `"balance":0`)

	var persisted model.Channel
	require.NoError(t, db.First(&persisted, "id = ?", 1).Error)
	require.Equal(t, common.ChannelStatusEnabled, persisted.Status)
	require.Equal(t, 7.0, persisted.Balance)
	require.Equal(t, int64(123), persisted.BalanceUpdatedTime)
}

func TestDiscoverChannelModelsReturnsReadOnlyUpstreamList(t *testing.T) {
	db := setupModelListControllerTestDB(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/models", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":[{"id":"gpt-4o"},{"id":"gpt-4.1-mini"}]}`))
	}))
	defer server.Close()

	baseURL := server.URL
	channel := &model.Channel{
		Id:      2,
		Name:    "discovery-target",
		Type:    constant.ChannelTypeOpenAI,
		Key:     "secret",
		Status:  common.ChannelStatusEnabled,
		BaseURL: &baseURL,
		Group:   "default",
		Models:  "gpt-4o",
	}
	require.NoError(t, db.Create(channel).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "id", Value: "2"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/channel/discover_models/2", nil)

	DiscoverChannelModels(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), `"success":true`)
	require.Contains(t, recorder.Body.String(), `"gpt-4o"`)
	require.Contains(t, recorder.Body.String(), `"gpt-4.1-mini"`)
}
