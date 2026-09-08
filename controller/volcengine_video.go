package controller

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/doubao"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func RelayVolcengineVideoTaskFetch(c *gin.Context) {
	task, exists, err := model.GetByTaskId(c.GetInt("id"), c.Param("task_id"))
	if err != nil {
		respondTaskError(c, service.TaskErrorWrapperLocal(errors.New("failed to load task"), "internal_error", http.StatusInternalServerError))
		return
	}
	if !exists || task.Platform != constant.TaskPlatformVolcengineVideo {
		respondTaskError(c, service.TaskErrorWrapperLocal(errors.New("task not found"), "NotFound", http.StatusNotFound))
		return
	}
	if !volcengineVideoTokenAllowsTask(c, task) {
		respondTaskError(c, service.TaskErrorWrapperLocal(errors.New("token has no access to this model"), "model_forbidden", http.StatusForbidden))
		return
	}
	body, taskErr := requestVolcengineVideoTask(c, task, http.MethodGet, task.GetUpstreamTaskID(), nil)
	if taskErr != nil {
		respondTaskError(c, taskErr)
		return
	}
	adaptor := &doubao.NativeTaskAdaptor{}
	result, err := adaptor.ParseTaskResult(body)
	if err != nil || result.TaskID != task.GetUpstreamTaskID() {
		respondTaskError(c, service.TaskErrorWrapperLocal(errors.New("invalid upstream task response"), "upstream_error", http.StatusBadGateway))
		return
	}
	if err := service.ApplyVideoTaskResponse(c, adaptor, task, body); err != nil {
		respondTaskError(c, service.TaskErrorWrapperLocal(errors.New("failed to update task"), "internal_error", http.StatusInternalServerError))
		return
	}
	if c.Request.Method == http.MethodDelete {
		// Settle an already completed task before removing its upstream usage record.
		// DELETE success alone never authorizes a refund: polling must confirm cancellation.
		body, taskErr = requestVolcengineVideoTask(c, task, http.MethodDelete, task.GetUpstreamTaskID(), nil)
		if taskErr != nil {
			respondTaskError(c, taskErr)
			return
		}
	}
	c.Data(http.StatusOK, "application/json", body)
}

func RelayVolcengineVideoTaskList(c *gin.Context) {
	page, pageErr := strconv.Atoi(c.DefaultQuery("page_num", "1"))
	size, sizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageErr != nil || sizeErr != nil || page < 1 || page > 500 || size < 1 || size > 500 {
		respondTaskError(c, service.TaskErrorWrapperLocal(errors.New("page_num and page_size must be between 1 and 500"), "InvalidParameter", http.StatusBadRequest))
		return
	}
	query := url.Values{"page_num": {"1"}, "page_size": {"500"}}
	for _, field := range []string{"filter.model", "filter.status", "filter.service_tier"} {
		if value := c.Query(field); value != "" {
			query.Set(field, value)
		}
	}
	switch c.Query("filter.status") {
	case "", "queued", "running", "succeeded", "failed", "cancelled", "expired":
	default:
		respondTaskError(c, service.TaskErrorWrapperLocal(errors.New("invalid filter.status"), "InvalidParameter", http.StatusBadRequest))
		return
	}
	switch c.Query("filter.service_tier") {
	case "", "default", "flex":
	default:
		respondTaskError(c, service.TaskErrorWrapperLocal(errors.New("invalid filter.service_tier"), "InvalidParameter", http.StatusBadRequest))
		return
	}
	tasks, err := model.GetVolcengineVideoTasks(c.GetInt("id"), c.QueryArray("filter.task_ids"))
	if err != nil {
		respondTaskError(c, service.TaskErrorWrapperLocal(errors.New("failed to load tasks"), "internal_error", http.StatusInternalServerError))
		return
	}
	type credential struct {
		channelID int
		key       string
	}
	groups := make(map[credential][]*model.Task)
	for _, task := range tasks {
		if !volcengineVideoTokenAllowsTask(c, task) {
			continue
		}
		key := credential{task.ChannelId, task.PrivateData.Key}
		groups[key] = append(groups[key], task)
	}
	type item struct {
		data      json.RawMessage
		id        string
		createdAt int64
	}
	items := make([]item, 0)
	adaptor := &doubao.NativeTaskAdaptor{}
	// ponytail: one upstream list call per credential and 50 owned tasks;
	// index cached native snapshots if listing large histories becomes expensive.
	// Keep repeated task IDs below typical upstream request-URI limits.
	const batchSize = 50
	for _, group := range groups {
		for start := 0; start < len(group); start += batchSize {
			batch := group[start:min(start+batchSize, len(group))]
			owned := make(map[string]*model.Task, len(batch))
			query.Del("filter.task_ids")
			for _, task := range batch {
				owned[task.GetUpstreamTaskID()] = task
				query.Add("filter.task_ids", task.GetUpstreamTaskID())
			}
			body, taskErr := requestVolcengineVideoTask(c, batch[0], http.MethodGet, "", query)
			if taskErr != nil {
				respondTaskError(c, taskErr)
				return
			}
			var response struct {
				Items []json.RawMessage `json:"items"`
				Total *int              `json:"total"`
			}
			if common.Unmarshal(body, &response) != nil || response.Total == nil || *response.Total < 0 || *response.Total > len(response.Items) {
				respondTaskError(c, service.TaskErrorWrapperLocal(errors.New("invalid upstream task list"), "upstream_error", http.StatusBadGateway))
				return
			}
			for _, raw := range response.Items {
				var metadata struct {
					ID        string `json:"id"`
					CreatedAt int64  `json:"created_at"`
				}
				if common.Unmarshal(raw, &metadata) != nil {
					respondTaskError(c, service.TaskErrorWrapperLocal(errors.New("invalid upstream task list item"), "upstream_error", http.StatusBadGateway))
					return
				}
				task, ok := owned[metadata.ID]
				if !ok {
					// Upstream filtering is not an authorization boundary.
					continue
				}
				delete(owned, metadata.ID)
				if err := service.ApplyVideoTaskResponse(c, adaptor, task, raw); err != nil {
					respondTaskError(c, service.TaskErrorWrapperLocal(errors.New("failed to update task"), "internal_error", http.StatusInternalServerError))
					return
				}
				items = append(items, item{raw, metadata.ID, metadata.CreatedAt})
			}
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].createdAt == items[j].createdAt {
			return items[i].id > items[j].id
		}
		return items[i].createdAt > items[j].createdAt
	})
	start := min((page-1)*size, len(items))
	result := make([]json.RawMessage, 0, size)
	for _, item := range items[start:min(start+size, len(items))] {
		result = append(result, item.data)
	}
	c.JSON(http.StatusOK, gin.H{"items": result, "total": len(items)})
}

func volcengineVideoTokenAllowsTask(c *gin.Context, task *model.Task) bool {
	if !common.GetContextKeyBool(c, constant.ContextKeyTokenModelLimitEnabled) {
		return true
	}
	limits, _ := common.GetContextKeyType[map[string]bool](c, constant.ContextKeyTokenModelLimit)
	_, allowed := limits[ratio_setting.FormatMatchingModelName(task.Properties.OriginModelName)]
	return allowed
}

func requestVolcengineVideoTask(c *gin.Context, task *model.Task, method, taskID string, query url.Values) ([]byte, *dto.TaskError) {
	ch, err := model.GetChannelById(task.ChannelId, true)
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(errors.New("task channel unavailable"), "internal_error", http.StatusInternalServerError)
	}
	baseURL := ch.GetBaseURL()
	if baseURL == "" {
		baseURL = constant.ChannelBaseURLs[ch.Type]
	}
	key := task.PrivateData.Key
	if key == "" {
		key = ch.Key
	}
	adaptor := &doubao.NativeTaskAdaptor{}
	adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: baseURL, ApiKey: key}})
	resp, err := adaptor.QueryTask(c.Request.Context(), method, taskID, query, ch.GetSetting().Proxy)
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(errors.New("upstream task request failed"), "upstream_error", http.StatusBadGateway)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(errors.New("failed to read upstream task response"), "upstream_error", http.StatusBadGateway)
	}
	if resp.StatusCode != http.StatusOK {
		taskErr := service.TaskErrorWrapperLocal(errors.New("upstream task request failed"), "upstream_error", http.StatusBadGateway)
		var errorResponse struct {
			Error json.RawMessage `json:"error"`
		}
		if common.Unmarshal(body, &errorResponse) == nil && common.GetJsonType(errorResponse.Error) == "object" {
			taskErr.StatusCode = resp.StatusCode
			taskErr.Data = errorResponse.Error
		}
		return nil, taskErr
	}
	return body, nil
}
