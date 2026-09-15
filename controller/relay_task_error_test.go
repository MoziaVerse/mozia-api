package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVDNOnlyRetriesExplicitOverloadRejection(t *testing.T) {
	for _, tc := range []struct {
		name        string
		channelType int
		code        string
		status      int
		want        bool
	}{
		{"overload", constant.ChannelTypeMoziaH3VDN, "fail_to_fetch_task", 429, true},
		{"network acceptance unknown", constant.ChannelTypeMoziaH3VDN, "do_request_failed", 500, false},
		{"response truncated", constant.ChannelTypeMoziaH3VDN, "read_response_body_failed", 500, false},
		{"invalid submit response", constant.ChannelTypeMoziaH3VDN, "unmarshal_response_failed", 500, false},
		{"server error", constant.ChannelTypeMoziaH3VDN, "fail_to_fetch_task", 500, false},
		{"timeout", constant.ChannelTypeMoziaH3VDN, "fail_to_fetch_task", 504, false},
		{"auth", constant.ChannelTypeMoziaH3VDN, "fail_to_fetch_task", 401, false},
		{"invalid body", constant.ChannelTypeMoziaH3VDN, "fail_to_fetch_task", 422, false},
		{"existing H3 retry unchanged", constant.ChannelTypeMoziaH3, "do_request_failed", 500, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Set("channel_type", tc.channelType)
			taskErr := &dto.TaskError{Code: tc.code, StatusCode: tc.status}
			assert.Equal(t, tc.want, shouldRetryTaskRelay(ctx, 1, taskErr, 1))
			assert.False(t, shouldRetryTaskRelay(ctx, 1, taskErr, 0))
			ctx.Set("specific_channel_id", 1)
			assert.False(t, shouldRetryTaskRelay(ctx, 1, taskErr, 1))
		})
	}
}

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
