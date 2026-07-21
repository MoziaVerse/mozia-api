package relay

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyTaskParamOverrideBeforeValidationAndRestoreForVideoRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, path := range []string{"/v1/videos", "/v1/video/generations"} {
		t.Run(path, func(t *testing.T) {
			originalJSON := []byte(`{"model":"video-model","prompt":"test","seconds":"5"}`)
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(originalJSON))
			ctx.Request.Header.Set("Content-Type", "application/json")

			originalStorage, err := common.GetBodyStorage(ctx)
			require.NoError(t, err)
			t.Cleanup(func() { _ = originalStorage.Close() })

			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{
				ParamOverride: map[string]interface{}{
					"operations": []interface{}{map[string]interface{}{
						"mode":  "set",
						"path":  "seconds",
						"value": 15,
					}},
				},
			}}
			restore, taskErr := applyTaskParamOverride(ctx, info)
			require.Nil(t, taskErr)
			require.NotNil(t, restore)

			var effectiveRequest relaycommon.TaskSubmitReq
			require.NoError(t, common.UnmarshalBodyReusable(ctx, &effectiveRequest))
			assert.Equal(t, "15", effectiveRequest.Seconds)

			effectiveStorage, err := common.GetBodyStorage(ctx)
			require.NoError(t, err)
			effectiveJSON, err := effectiveStorage.Bytes()
			require.NoError(t, err)
			assert.JSONEq(t, `{"model":"video-model","prompt":"test","seconds":15}`, string(effectiveJSON))

			restore()
			restoredStorage, err := common.GetBodyStorage(ctx)
			require.NoError(t, err)
			assert.Same(t, originalStorage, restoredStorage)
			restoredJSON, err := restoredStorage.Bytes()
			require.NoError(t, err)
			assert.Equal(t, originalJSON, restoredJSON)
		})
	}
}
