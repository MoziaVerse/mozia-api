package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupplierStatsSeparateAttemptsAndFinalOutcome(t *testing.T) {
	db := setupMaterialControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SupplierAttempt{}))
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedis })
	now := time.Now().Unix()
	attempts := []model.SupplierAttempt{
		{RequestID: "retry-success", Attempt: 1, SupplierID: 1, PoolID: 1, Model: "test", GroupName: "default", Kind: "first", Status: "failed", CreatedAt: now},
		{RequestID: "retry-success", Attempt: 2, SupplierID: 2, PoolID: 2, Model: "test", GroupName: "default", Kind: "retry", Status: "success", CreatedAt: now},
		{RequestID: "direct-success", Attempt: 1, SupplierID: 2, PoolID: 2, Model: "test", GroupName: "default", Kind: "first", Status: "success", CreatedAt: now},
		{RequestID: "probe", Attempt: 1, SupplierID: 1, PoolID: 1, Model: "test", GroupName: "default", Kind: "probe", Status: "success", CreatedAt: now},
		{RequestID: "shadow", Attempt: 0, SupplierID: 1, PoolID: 1, Model: "test", GroupName: "default", Kind: "shadow", Status: "observed", CreatedAt: now},
	}
	require.NoError(t, db.Create(&attempts).Error)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/channel/supplier-routing/stats", nil)
	GetSupplierRoutingStats(c)
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Counts map[string]int64 `json:"summary"`
			Rows   []struct {
				Kind  string  `json:"kind"`
				Share float64 `json:"first_share_percent"`
			} `json:"rows"`
		} `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, recorder.Body.String())
	assert.Equal(t, int64(2), response.Data.Counts["requests"])
	assert.Equal(t, int64(1), response.Data.Counts["first_success"])
	assert.Equal(t, int64(2), response.Data.Counts["final_success"])
	for _, row := range response.Data.Rows {
		if row.Kind == "first" {
			assert.Equal(t, float64(50), row.Share)
		} else {
			assert.Zero(t, row.Share)
		}
	}
}

func TestSupplierRetryBoundsPrecedeChannelErrorFastPath(t *testing.T) {
	for _, scenario := range []string{"attempts-exhausted", "deadline", "already-written", "specified-channel", "skip-retry"} {
		t.Run(scenario, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			remaining := 2
			err := types.NewErrorWithStatusCode(assert.AnError, types.ErrorCode("channel:test"), 502)
			switch scenario {
			case "attempts-exhausted":
				remaining = 0
			case "deadline":
				ctx, cancel := context.WithCancel(c.Request.Context())
				cancel()
				c.Request = c.Request.WithContext(ctx)
			case "already-written":
				c.Writer.WriteHeaderNow()
			case "specified-channel":
				c.Set("specific_channel_id", 1)
			case "skip-retry":
				err = types.NewErrorWithStatusCode(assert.AnError, types.ErrorCode("channel:test"), 502, types.ErrOptionWithSkipRetry())
			}
			assert.False(t, shouldRetry(c, err, remaining))
		})
	}
}

func TestSupplierPublicationCannotUseGenericOptions(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPut, "/api/option", strings.NewReader(`{"key":"SupplierRoutingPublication","value":"{}"}`))
	UpdateOption(c)
	assert.Contains(t, recorder.Body.String(), "dedicated publication endpoint")
}

func TestSupplierProcurementSummaryPermissionAndRetryCosts(t *testing.T) {
	db := setupMaterialControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SupplierAttempt{}))
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedis })
	now := time.Now().Unix()
	rows := []model.SupplierAttempt{
		{RequestID: "r", Attempt: 1, Kind: "first", Status: "failed", Currency: "CNY", Cost: "0.1", CostStatus: "calculated", CreatedAt: now},
		{RequestID: "r", Attempt: 2, Kind: "retry", Status: "success", Currency: "CNY", Cost: "0.2", CostStatus: "reconciled", CreatedAt: now},
		{RequestID: "u", Attempt: 1, Kind: "first", Status: "unknown", Currency: "CNY", CostStatus: "pending", CreatedAt: now},
		{RequestID: "p", Attempt: 1, Kind: "probe", Status: "success", Currency: "CNY", Cost: "10", CostStatus: "calculated", CreatedAt: now},
	}
	require.NoError(t, db.Create(&rows).Error)
	for _, role := range []int{common.RoleCommonUser, common.RoleRootUser} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/stats", nil)
		c.Set("role", role)
		c.Set("id", 1)
		GetSupplierRoutingStats(c)
		var response struct {
			Success bool
			Data    struct {
				Costs []struct {
					Total   string
					Retry   string `json:"retry_cost"`
					Failed  string `json:"failed_cost"`
					Pending int
					Success int `json:"successful_requests"`
				}
			}
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success, recorder.Body.String())
		if role == common.RoleCommonUser {
			assert.Empty(t, response.Data.Costs)
			continue
		}
		require.Len(t, response.Data.Costs, 1)
		cost := response.Data.Costs[0]
		total, err := strconv.ParseFloat(cost.Total, 64)
		require.NoError(t, err)
		assert.InDelta(t, 0.3, total, 1e-8)
		assert.Equal(t, "0.2", cost.Retry)
		assert.Equal(t, "0.1", cost.Failed)
		assert.Equal(t, 1, cost.Pending)
		assert.Equal(t, 1, cost.Success)
	}
}

func TestSupplierHistoryRangeFiltersAndPagination(t *testing.T) {
	db := setupMaterialControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SupplierAttempt{}))
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedis })
	rows := []model.SupplierAttempt{
		{RequestID: "prior-first", Attempt: 1, SupplierID: 1, Model: "test", Kind: "first", Status: "failed", CreatedAt: 99, CostStatus: "calculated", Cost: "10", Currency: "CNY"},
		{RequestID: "prior-first", Attempt: 2, SupplierID: 2, Model: "test", Kind: "retry", Status: "success", CreatedAt: 100, CostStatus: "calculated", Cost: "0.2", Currency: "CNY"},
		{RequestID: "same", Attempt: 1, SupplierID: 2, Model: "test", Kind: "first", Status: "failed", CreatedAt: 101, CostStatus: "calculated", Cost: "0.1", Currency: "CNY"},
		{RequestID: "same", Attempt: 2, SupplierID: 2, Model: "test", Kind: "retry", Status: "success", CreatedAt: 102, CostStatus: "calculated", Cost: "0.2", Currency: "CNY"},
		{RequestID: "shadow", Attempt: 0, SupplierID: 2, Model: "test", Kind: "shadow", Status: "observed", CreatedAt: 103},
		{RequestID: "other-model", Attempt: 1, SupplierID: 2, Model: "other", Kind: "first", Status: "success", CreatedAt: 103, CostStatus: "calculated", Cost: "20", Currency: "CNY"},
		{RequestID: "other-supplier", Attempt: 1, SupplierID: 1, Model: "test", Kind: "first", Status: "success", CreatedAt: 103, CostStatus: "calculated", Cost: "20", Currency: "CNY"},
		{RequestID: "end", Attempt: 1, SupplierID: 2, Model: "test", Kind: "first", Status: "success", CreatedAt: 104, CostStatus: "calculated", Cost: "20", Currency: "CNY"},
	}
	require.NoError(t, db.Create(&rows).Error)
	filter := "?start_timestamp=100&end_timestamp=104&supplier_id=2&model=test"
	for page, expectedIDs := range map[int][]int64{1: {rows[3].ID, rows[2].ID}, 2: {rows[1].ID}} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/attempts"+filter+"&page_size=2&p="+strconv.Itoa(page), nil)
		GetSupplierAttempts(c)
		var response struct {
			Success bool
			Data    struct {
				Total int
				Items []model.SupplierAttempt
			}
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.True(t, response.Success, recorder.Body.String())
		assert.Equal(t, 3, response.Data.Total)
		ids := []int64{}
		for _, row := range response.Data.Items {
			ids = append(ids, row.ID)
		}
		assert.Equal(t, expectedIDs, ids)
	}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/stats"+filter, nil)
	c.Set("role", common.RoleRootUser)
	c.Set("id", 1)
	GetSupplierRoutingStats(c)
	var response struct {
		Success bool
		Data    struct {
			Summary struct {
				Requests int
				Final    int `json:"final_success"`
			}
			Costs []struct{ Total string }
		}
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success, recorder.Body.String())
	assert.Equal(t, 2, response.Data.Summary.Requests, "a matching retry counts even when its first attempt is outside the filter")
	assert.Equal(t, 2, response.Data.Summary.Final)
	require.Len(t, response.Data.Costs, 1)
	assert.Equal(t, "0.5", response.Data.Costs[0].Total)
	for _, query := range []string{"?start_timestamp=104&end_timestamp=100", "?start_timestamp=bad", "?supplier_id=-1", "?p=-1", "?p=1&page_size=0"} {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = httptest.NewRequest(http.MethodGet, "/attempts"+query, nil)
		GetSupplierAttempts(c)
		assert.Equal(t, http.StatusBadRequest, recorder.Code, query)
	}
	recorder = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "/realtime", nil)
	GetSupplierRealtime(c)
	assert.Contains(t, recorder.Body.String(), `"available":false`)
}
