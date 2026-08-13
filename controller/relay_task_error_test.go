package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRespondTaskErrorAddsType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name        string
		taskErr     *dto.TaskError
		wantStatus  int
		wantType    string
		wantMessage string
	}{
		{
			name: "bad request",
			taskErr: &dto.TaskError{
				Code:       "task_not_exist",
				Message:    "task_not_exist",
				StatusCode: http.StatusBadRequest,
			},
			wantStatus:  http.StatusBadRequest,
			wantType:    "invalid_request_error",
			wantMessage: "task_not_exist",
		},
		{
			name: "rate limit",
			taskErr: &dto.TaskError{
				Code:       "rate_limit",
				Message:    "upstream overloaded",
				StatusCode: http.StatusTooManyRequests,
			},
			wantStatus:  http.StatusTooManyRequests,
			wantType:    "rate_limit_error",
			wantMessage: "当前分组上游负载已饱和，请稍后再试",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)

			respondTaskError(c, tt.taskErr)

			require.Equal(t, tt.wantStatus, recorder.Code)
			assert.JSONEq(t, `{
				"code": "`+tt.taskErr.Code+`",
				"message": "`+tt.wantMessage+`",
				"type": "`+tt.wantType+`",
				"data": null
			}`, recorder.Body.String())
		})
	}
}
