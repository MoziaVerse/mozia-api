package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelMetadataPreservesLegacyUpdatesAndPublishesExplicitChanges(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	groups := setting.UserUsableGroups2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(groups))
		model.InvalidatePricingCache()
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	router := gin.New()
	router.POST("/api/models/", CreateModelMeta)
	router.PUT("/api/models/", UpdateModelMeta)
	router.GET("/api/pricing", GetPricing)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/models/", strings.NewReader(
		`{"model_name":"metadata-model","tags":"category:text","status":1,"function_tags":"Reasoning","max_prompt_tokens":131072,"max_completion_tokens":8192}`,
	)))
	var created struct {
		Success bool        `json:"success"`
		Data    model.Model `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &created))
	require.True(t, created.Success, recorder.Body.String())
	require.Positive(t, created.Data.Id)
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "metadata-model", ChannelId: 1, Enabled: true}).Error)

	for _, tc := range []struct {
		name       string
		fields     string
		wantPrompt int64
		wantOutput int64
		wantTags   string
	}{
		{"legacy client", "", 131072, 8192, "Reasoning"},
		{"partial metadata", `,"max_prompt_tokens":262144`, 262144, 8192, "Reasoning"},
		{"explicit clear", `,"function_tags":"","max_prompt_tokens":null,"max_completion_tokens":null`, 0, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"id":%d,"model_name":"metadata-model","description":"edited","tags":"category:text","status":1%s}`, created.Data.Id, tc.fields)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/models/", strings.NewReader(body)))
			var updated struct{ Success bool }
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &updated))
			require.True(t, updated.Success, recorder.Body.String())

			var stored model.Model
			require.NoError(t, db.First(&stored, created.Data.Id).Error)
			assert.Equal(t, "edited", stored.Description)
			assert.Equal(t, tc.wantTags, stored.FunctionTags)
			if tc.wantPrompt == 0 {
				assert.Nil(t, stored.MaxPromptTokens)
				assert.Nil(t, stored.MaxCompletionTokens)
			} else {
				require.NotNil(t, stored.MaxPromptTokens)
				require.NotNil(t, stored.MaxCompletionTokens)
				assert.Equal(t, tc.wantPrompt, *stored.MaxPromptTokens)
				assert.Equal(t, tc.wantOutput, *stored.MaxCompletionTokens)
			}

			recorder = httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/pricing", nil))
			pricing := pricingByModelName(decodePricingResponse(t, recorder))
			require.Contains(t, pricing, "metadata-model")
			assert.Equal(t, stored.FunctionTags, pricing["metadata-model"].FunctionTags)
			assert.Equal(t, stored.MaxPromptTokens, pricing["metadata-model"].MaxPromptTokens)
			assert.Equal(t, stored.MaxCompletionTokens, pricing["metadata-model"].MaxCompletionTokens)
			assert.Equal(t, "text", pricing["metadata-model"].ModelCategory)
		})
	}
}

func TestModelMetadataRejectsInvalidCapacityBeforeSaving(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	router := gin.New()
	router.POST("/api/models/", CreateModelMeta)
	router.PUT("/api/models/", UpdateModelMeta)
	for _, field := range []string{
		`"max_prompt_tokens":-1`,
		`"max_completion_tokens":0`,
		`"max_prompt_tokens":1.5`,
		`"max_completion_tokens":9007199254740992`,
		`"function_tags":"` + strings.Repeat("x", 513) + `"`,
	} {
		for _, method := range []string{http.MethodPost, http.MethodPut} {
			body := `{"id":1,"model_name":"invalid-metadata","tags":"category:text",` + field + `}`
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(method, "/api/models/", strings.NewReader(body)))
			var response struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			assert.False(t, response.Success, "%s: %s", method, body)
			assert.NotEmpty(t, response.Message)
		}
	}
	var count int64
	require.NoError(t, db.Model(&model.Model{}).Count(&count).Error)
	assert.Zero(t, count)
}
