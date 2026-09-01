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

func TestTaskBillingEvaluationUsesExplicitModelConfiguration(t *testing.T) {
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
		Surcharge: &taskbilling.Surcharge{
			Name: "images", Kind: taskbilling.SurchargeItemCount, Paths: []string{"images"}, FreeCount: 5, UnitPrice: 0.2,
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
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewBufferString(`{"duration":4.2,"images":["1","2","3","4","5","6"]}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.GetBodyStorage(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })

	evaluation, rule, isConfigured, err := TaskBillingEvaluation(ctx, model)
	require.NoError(t, err)
	assert.True(t, isConfigured)
	assert.Equal(t, taskbilling.ModePerSecond, rule.Mode)
	assert.Equal(t, map[string]float64{"duration": 5}, evaluation.Ratios)
	require.NotNil(t, evaluation.Surcharge)
	assert.Equal(t, 1, evaluation.Surcharge.BillableCount)
	assert.InDelta(t, 0.2, evaluation.Surcharge.Price, 0.000001)

	_, _, isConfigured, err = TaskBillingEvaluation(ctx, "unconfigured-task-billing-model")
	require.NoError(t, err)
	assert.False(t, isConfigured)
}

func TestTaskBillingEvaluationPerRequestDoesNotReadTheBody(t *testing.T) {
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

	evaluation, rule, isConfigured, err := TaskBillingEvaluation(nil, model)
	require.NoError(t, err)
	assert.True(t, isConfigured)
	assert.Equal(t, taskbilling.ModePerRequest, rule.Mode)
	assert.Nil(t, evaluation.Ratios)
}

func TestTaskBillingEvaluationTokenParametricDetectsReferenceVideo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const modelName = "token-parametric-task-model"
	referencePrice := 21.0
	original, err := common.Marshal(billing_setting.GetTaskBillingCopy())
	require.NoError(t, err)
	configured, err := common.Marshal(map[string]taskbilling.Config{modelName: {
		Version: taskbilling.Version1,
		Mode:    taskbilling.ModeTokenParametric,
		TokenPrices: &taskbilling.TokenPriceTable{
			Paths: []string{"resolution"},
			Values: map[string]taskbilling.TokenUnitPrice{
				"480p": {Standard: 34.5, ReferenceVideo: &referencePrice},
			},
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
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", bytes.NewBufferString(`{
		"resolution":"480p",
		"content":[{"type":"video_url","role":"reference_video","video_url":{"url":"https://example.com/ref.mp4"}}]
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.GetBodyStorage(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })

	evaluation, rule, configuredRule, err := TaskBillingEvaluation(ctx, modelName)
	require.NoError(t, err)
	assert.True(t, configuredRule)
	assert.Equal(t, taskbilling.ModeTokenParametric, rule.Mode)
	require.NotNil(t, evaluation.TokenPrice)
	assert.Equal(t, &taskbilling.TokenPriceResult{
		Resolution: "480p", ReferenceVideo: true, UnitPrice: 21,
	}, evaluation.TokenPrice)
}
