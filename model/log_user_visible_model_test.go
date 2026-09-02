package model

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestApplyUserVisibleModelToLog(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	common.SetContextKey(c, constant.ContextKeyUserVisibleModel, "vendor/source")
	other := map[string]interface{}{
		"requested_model":     "vendor/source",
		"effective_model":     "vendor/target",
		"upstream_model_name": "provider-native-target",
	}

	modelName, other := applyUserVisibleModelToLog(c, "vendor/target", other)

	assert.Equal(t, "vendor/source", modelName)
	assert.NotContains(t, other, "effective_model")
	assert.NotContains(t, other, "upstream_model_name")
	assert.Equal(t, "vendor/target", other["admin_info"].(map[string]interface{})["effective_model"])
}
