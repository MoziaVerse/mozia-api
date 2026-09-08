package doubao

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// NativeTaskAdaptor preserves Ark JSON while using the existing task billing and polling lifecycle.
type NativeTaskAdaptor struct {
	TaskAdaptor
}

func (a *NativeTaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if !common.SupportsVolcengineVideo(info.ChannelType) {
		return service.TaskErrorWrapperLocal(fmt.Errorf("channel does not support native Volcengine video"), "unsupported_channel", http.StatusBadRequest)
	}
	var input struct {
		Model    string        `json:"model"`
		Content  []ContentItem `json:"content"`
		Duration *int          `json:"duration,omitempty"`
	}
	if err := common.UnmarshalBodyReusable(c, &input); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if strings.TrimSpace(input.Model) == "" || len(input.Content) == 0 {
		return service.TaskErrorWrapperLocal(fmt.Errorf("model and non-empty content are required"), "invalid_request", http.StatusBadRequest)
	}
	for _, item := range input.Content {
		if strings.TrimSpace(item.Type) == "" {
			return service.TaskErrorWrapperLocal(fmt.Errorf("content items must have a type"), "invalid_request", http.StatusBadRequest)
		}
	}
	info.Action = constant.TaskActionGenerate
	return nil
}

func (a *NativeTaskAdaptor) EstimateBilling(_ *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	return nil
}

func nativeTaskURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	baseURL = strings.TrimSuffix(baseURL, "/v1")
	baseURL = strings.TrimSuffix(baseURL, "/api/v3")
	return baseURL + constant.VolcengineVideoTaskPath
}

func (a *NativeTaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return nativeTaskURL(a.baseURL), nil
}

func (a *NativeTaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	var payload map[string]json.RawMessage
	if err := common.UnmarshalBodyReusable(c, &payload); err != nil {
		return nil, err
	}
	modelJSON, err := common.Marshal(info.UpstreamModelName)
	if err != nil {
		return nil, err
	}
	payload["model"] = modelJSON
	data, err := common.Marshal(payload)
	return bytes.NewReader(data), err
}

func (a *NativeTaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *NativeTaskAdaptor) DoResponse(_ *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_failed", http.StatusBadGateway)
	}
	var submitted responsePayload
	if err := common.Unmarshal(body, &submitted); err != nil || strings.TrimSpace(submitted.ID) == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("upstream did not return a task id"), "invalid_response", http.StatusBadGateway)
	}
	// Native callbacks and draft-task references use the same id as the upstream.
	info.PublicTaskID = submitted.ID
	return submitted.ID, body, nil
}

func (a *NativeTaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, _ := body["task_id"].(string)
	if strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	queryAdaptor := *a
	queryAdaptor.baseURL, queryAdaptor.apiKey = baseURL, key
	return queryAdaptor.QueryTask(context.Background(), http.MethodGet, taskID, nil, proxy)
}

// QueryTask serves native get/list/delete with the credentials used at submission.
func (a *NativeTaskAdaptor) QueryTask(ctx context.Context, method, taskID string, query url.Values, proxy string) (*http.Response, error) {
	requestURL := nativeTaskURL(a.baseURL)
	if taskID != "" {
		requestURL += "/" + url.PathEscape(taskID)
	}
	if len(query) != 0 {
		requestURL += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Accept", "application/json")
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, err
	}
	return client.Do(req)
}

func (a *NativeTaskAdaptor) GetChannelName() string {
	return string(constant.TaskPlatformVolcengineVideo)
}
