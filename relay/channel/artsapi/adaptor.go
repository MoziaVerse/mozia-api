package artsapi

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/seedance"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

const channelName = "artsapi"

// TaskAdaptor keeps ArtsAPI-specific request and response behavior isolated
// while reusing the compatible /v1/video/generations task lifecycle.
type TaskAdaptor struct {
	seedance.TaskAdaptor
}

type taskRequest struct {
	Prompt   string `json:"prompt"`
	Model    string `json:"model,omitempty"`
	Duration *int   `json:"duration,omitempty"`
}

type taskUsageResponse struct {
	Usage struct {
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func (a *TaskAdaptor) GetChannelName() string {
	return channelName
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if strings.TrimSpace(info.ChannelBaseUrl) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("channel base URL is required"), "invalid_channel_base_url", http.StatusBadRequest)
	}

	var input taskRequest
	if err := common.UnmarshalBodyReusable(c, &input); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if strings.TrimSpace(input.Prompt) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest)
	}

	if input.Duration == nil || *input.Duration <= 0 {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("duration is required and must be a positive number of seconds"),
			"invalid_duration",
			http.StatusBadRequest,
		)
	}

	req := relaycommon.TaskSubmitReq{
		Prompt:   input.Prompt,
		Model:    input.Model,
		Duration: *input.Duration,
	}

	info.Action = "generate"
	c.Set("task_request", req)
	return nil
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	result, err := a.TaskAdaptor.ParseTaskResult(respBody)
	if err != nil {
		return nil, err
	}

	var response taskUsageResponse
	if err := common.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("decode ArtsAPI task usage: %w", err)
	}
	result.CompletionTokens = response.Usage.CompletionTokens
	result.TotalTokens = response.Usage.TotalTokens
	if result.TotalTokens == 0 {
		result.TotalTokens = result.CompletionTokens
	}
	return result, nil
}
