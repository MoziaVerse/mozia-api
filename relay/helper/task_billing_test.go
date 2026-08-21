package helper

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/taskbilling"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskBillingRatiosUsesExplicitModelConfiguration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const model = "task-billing-test-model"
	original, err := common.Marshal(billing_setting.GetTaskBillingCopy())
	require.NoError(t, err)
	configured, err := common.Marshal(map[string]taskbilling.Config{model: {
		Version: taskbilling.Version1,
		Mode:    taskbilling.ModePerSecond,
		Duration: &taskbilling.Dimension{
			Paths: []string{"duration", "seconds"},
			Round: taskbilling.RoundCeil,
		},
	}})
	require.NoError(t, err)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.task_billing": string(configured),
	}))
	t.Cleanup(func() {
		_ = config.GlobalConfig.LoadFromDB(map[string]string{
			"billing_setting.task_billing": string(original),
		})
	})

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewBufferString(`{"duration":4.2}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.GetBodyStorage(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })

	ratios, rule, isConfigured, err := TaskBillingRatios(ctx, model)
	require.NoError(t, err)
	assert.True(t, isConfigured)
	assert.Equal(t, taskbilling.ModePerSecond, rule.Mode)
	assert.Equal(t, map[string]float64{"duration": 5}, ratios)

	_, _, isConfigured, err = TaskBillingRatios(ctx, "unconfigured-task-billing-model")
	require.NoError(t, err)
	assert.False(t, isConfigured)
}

func TestTaskBillingRatiosPerRequestDoesNotReadTheBody(t *testing.T) {
	const model = "per-request-task-billing-test-model"
	original, err := common.Marshal(billing_setting.GetTaskBillingCopy())
	require.NoError(t, err)
	configured, err := common.Marshal(map[string]taskbilling.Config{model: {
		Version: taskbilling.Version1,
		Mode:    taskbilling.ModePerRequest,
	}})
	require.NoError(t, err)
	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.task_billing": string(configured),
	}))
	t.Cleanup(func() {
		_ = config.GlobalConfig.LoadFromDB(map[string]string{
			"billing_setting.task_billing": string(original),
		})
	})

	ratios, rule, isConfigured, err := TaskBillingRatios(nil, model)
	require.NoError(t, err)
	assert.True(t, isConfigured)
	assert.Equal(t, taskbilling.ModePerRequest, rule.Mode)
	assert.Nil(t, ratios)
}
