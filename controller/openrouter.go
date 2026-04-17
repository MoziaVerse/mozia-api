package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/ollama"

	"github.com/gin-gonic/gin"
)

type openRouterPrefillChannel struct {
	ChannelID       int    `json:"channel_id"`
	ChannelName     string `json:"channel_name"`
	ChannelType     int    `json:"channel_type"`
	ChannelTypeName string `json:"channel_type_name"`
	BaseURL         string `json:"base_url,omitempty"`
}

type openRouterPrefillValues struct {
	HuggingFaceID               string `json:"hugging_face_id,omitempty"`
	Name                        string `json:"name,omitempty"`
	Created                     int64  `json:"created,omitempty"`
	Description                 string `json:"description,omitempty"`
	ContextLength               int    `json:"context_length,omitempty"`
	MaxOutputLength             int    `json:"max_output_length,omitempty"`
	InputModalities             string `json:"input_modalities,omitempty"`
	OutputModalities            string `json:"output_modalities,omitempty"`
	Quantization                string `json:"quantization,omitempty"`
	Pricing                     string `json:"pricing,omitempty"`
	SupportedSamplingParameters string `json:"supported_sampling_parameters,omitempty"`
	SupportedFeatures           string `json:"supported_features,omitempty"`
	DeprecationDate             string `json:"deprecation_date,omitempty"`
	OpenRouterSlug              string `json:"openrouter_slug,omitempty"`
	Datacenters                 string `json:"datacenters,omitempty"`
}

type openRouterPrefillResponse struct {
	ModelID           int                        `json:"model_id"`
	ModelName         string                     `json:"model_name"`
	Channels          []openRouterPrefillChannel `json:"channels"`
	SelectedChannelID int                        `json:"selected_channel_id,omitempty"`
	Prefill           *openRouterPrefillValues   `json:"prefill,omitempty"`
	FilledFields      []string                   `json:"filled_fields,omitempty"`
	Warnings          []string                   `json:"warnings,omitempty"`
}

type openRouterUpstreamModel struct {
	ID          string `json:"id"`
	Created     int64  `json:"created"`
	MaxModelLen int    `json:"max_model_len,omitempty"`
}

type openRouterUpstreamModelsResponse struct {
	Data []openRouterUpstreamModel `json:"data"`
}

// GetAllOpenRouterModelMetas 管理端：分页获取 OpenRouter 模型元数据列表。
func GetAllOpenRouterModelMetas(c *gin.Context) {
	keyword := c.Query("keyword")
	pageInfo := common.GetPageQuery(c)
	metas, total, err := model.GetAllOpenRouterModelMetas(keyword, pageInfo.GetStartIdx(), pageInfo.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(metas)
	common.ApiSuccess(c, pageInfo)
}

// GetOpenRouterModelMeta 管理端：根据 ID 获取单条元数据。
func GetOpenRouterModelMeta(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	m, err := model.GetOpenRouterModelMetaByID(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, m)
}

// GetOpenRouterModelMetaPrefill 管理端：根据模型和渠道拉取上游信息并生成表单预填值。
func GetOpenRouterModelMetaPrefill(c *gin.Context) {
	modelID, err := strconv.Atoi(c.Query("model_id"))
	if err != nil || modelID <= 0 {
		common.ApiErrorMsg(c, "缺少合法的 model_id")
		return
	}

	var modelItem model.Model
	if err := model.DB.First(&modelItem, modelID).Error; err != nil {
		common.ApiErrorMsg(c, "关联的模型不存在")
		return
	}

	boundChannels, err := model.GetOpenRouterModelBoundChannels(modelItem.ModelName)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	resp := openRouterPrefillResponse{
		ModelID:   modelItem.Id,
		ModelName: modelItem.ModelName,
		Channels:  buildOpenRouterPrefillChannels(boundChannels),
	}

	channelIDStr := strings.TrimSpace(c.Query("channel_id"))
	if channelIDStr == "" {
		common.ApiSuccess(c, resp)
		return
	}

	channelID, err := strconv.Atoi(channelIDStr)
	if err != nil || channelID <= 0 {
		common.ApiErrorMsg(c, "缺少合法的 channel_id")
		return
	}

	channelAllowed := false
	for _, channel := range boundChannels {
		if channel.Id == channelID {
			channelAllowed = true
			break
		}
	}
	if !channelAllowed {
		common.ApiErrorMsg(c, "所选渠道未绑定该模型或当前不可用")
		return
	}

	channel, err := model.GetChannelById(channelID, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	prefill, filledFields, warnings, err := buildOpenRouterPrefill(modelItem, channel)
	if err != nil {
		common.ApiError(c, err)
		return
	}

	resp.SelectedChannelID = channelID
	resp.Prefill = prefill
	resp.FilledFields = filledFields
	resp.Warnings = warnings
	common.ApiSuccess(c, resp)
}

// CreateOpenRouterModelMeta 管理端：新增元数据。
func CreateOpenRouterModelMeta(c *gin.Context) {
	var m model.OpenRouterModelMeta
	if err := c.ShouldBindJSON(&m); err != nil {
		common.ApiError(c, err)
		return
	}
	if m.ModelId == 0 {
		common.ApiErrorMsg(c, "缺少 model_id")
		return
	}
	var existing model.Model
	if err := model.DB.First(&existing, m.ModelId).Error; err != nil {
		common.ApiErrorMsg(c, "关联的模型不存在")
		return
	}
	if dup, err := model.IsOpenRouterModelMetaDuplicated(0, m.ModelId); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "该模型已存在 OpenRouter 元数据")
		return
	}
	if err := validateOpenRouterModelMetaJSON(&m); err != nil {
		common.ApiError(c, err)
		return
	}
	m.Id = 0
	if err := m.Insert(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, &m)
}

// UpdateOpenRouterModelMeta 管理端：更新元数据。
func UpdateOpenRouterModelMeta(c *gin.Context) {
	var m model.OpenRouterModelMeta
	if err := c.ShouldBindJSON(&m); err != nil {
		common.ApiError(c, err)
		return
	}
	if m.Id == 0 {
		common.ApiErrorMsg(c, "缺少元数据 ID")
		return
	}
	if m.ModelId == 0 {
		common.ApiErrorMsg(c, "缺少 model_id")
		return
	}
	var existing model.Model
	if err := model.DB.First(&existing, m.ModelId).Error; err != nil {
		common.ApiErrorMsg(c, "关联的模型不存在")
		return
	}
	if dup, err := model.IsOpenRouterModelMetaDuplicated(m.Id, m.ModelId); err != nil {
		common.ApiError(c, err)
		return
	} else if dup {
		common.ApiErrorMsg(c, "该模型已存在 OpenRouter 元数据")
		return
	}
	if err := validateOpenRouterModelMetaJSON(&m); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := m.Update(); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, &m)
}

// DeleteOpenRouterModelMeta 管理端：删除元数据。
func DeleteOpenRouterModelMeta(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.DB.Delete(&model.OpenRouterModelMeta{}, id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, nil)
}

// ListOpenRouterModels 对外接口：返回所有可用模型，结构符合 OpenRouter
// "List Models" 规范。
//
//	GET /api/openrouter/models
func ListOpenRouterModels(c *gin.Context) {
	items, err := model.GetEnabledOpenRouterModels()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	data := make([]dto.OpenRouterModel, 0, len(items))
	for _, it := range items {
		data = append(data, toOpenRouterModel(it))
	}
	c.JSON(http.StatusOK, dto.OpenRouterListModelsResponse{Data: data})
}

func buildOpenRouterPrefillChannels(channels []model.OpenRouterModelBoundChannel) []openRouterPrefillChannel {
	items := make([]openRouterPrefillChannel, 0, len(channels))
	for _, channel := range channels {
		baseURL := channel.BaseURL
		if baseURL == "" && channel.Type >= 0 && channel.Type < len(constant.ChannelBaseURLs) {
			baseURL = constant.ChannelBaseURLs[channel.Type]
		}
		items = append(items, openRouterPrefillChannel{
			ChannelID:       channel.Id,
			ChannelName:     channel.Name,
			ChannelType:     channel.Type,
			ChannelTypeName: constant.GetChannelTypeName(channel.Type),
			BaseURL:         baseURL,
		})
	}
	return items
}

func buildOpenRouterPrefill(modelItem model.Model, channel *model.Channel) (*openRouterPrefillValues, []string, []string, error) {
	prefill := &openRouterPrefillValues{
		Name:        modelItem.ModelName,
		Description: modelItem.Description,
	}
	filledFields := make([]string, 0, 4)
	warnings := make([]string, 0, 3)

	if prefill.Name != "" {
		filledFields = append(filledFields, "name")
	}
	if prefill.Description != "" {
		filledFields = append(filledFields, "description")
	}

	switch channel.Type {
	case constant.ChannelTypeOllama:
		item, err := fetchOllamaModelForPrefill(channel, modelItem.ModelName)
		if err != nil {
			return nil, nil, nil, err
		}
		if created := parseOllamaModelCreated(item.ModifiedAt); created > 0 {
			prefill.Created = created
			filledFields = append(filledFields, "created")
		}
		if quantization := normalizeOllamaQuantization(item.Details.QuantizationLevel); quantization != "" {
			prefill.Quantization = quantization
			filledFields = append(filledFields, "quantization")
		}
	case constant.ChannelTypeGemini:
		warnings = append(warnings, "Gemini 渠道当前只能确认模型存在，暂不支持像 OpenAI 兼容渠道那样提取 created/context_length 等字段。")
	default:
		item, err := fetchOpenAICompatibleModelForPrefill(channel, modelItem.ModelName)
		if err != nil {
			return nil, nil, nil, err
		}
		if item.Created > 0 {
			prefill.Created = item.Created
			filledFields = append(filledFields, "created")
		}
		if item.MaxModelLen > 0 {
			prefill.ContextLength = item.MaxModelLen
			filledFields = append(filledFields, "context_length")
		}
	}

	if prefill.Created == 0 {
		warnings = append(warnings, "上游未返回可用的 created，未自动填充创建时间。")
	}
	if prefill.ContextLength == 0 {
		warnings = append(warnings, "上游未返回可用的 context_length/max_model_len，未自动填充上下文长度。")
	}
	if prefill.MaxOutputLength == 0 {
		warnings = append(warnings, "上游通常不会提供 max_output_length、pricing、modalities 等 OpenRouter 字段，这些字段仍需手动确认。")
	}

	return prefill, filledFields, warnings, nil
}

func fetchOpenAICompatibleModelForPrefill(channel *model.Channel, modelName string) (*openRouterUpstreamModel, error) {
	body, err := fetchOpenAICompatibleModelsBodyForPrefill(channel)
	if err != nil {
		return nil, err
	}

	var result openRouterUpstreamModelsResponse
	if err := common.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	for i := range result.Data {
		if result.Data[i].ID == modelName {
			return &result.Data[i], nil
		}
	}
	return nil, fmt.Errorf("渠道 %d 的上游模型列表中未找到模型 %s", channel.Id, modelName)
}

func fetchOpenAICompatibleModelsBodyForPrefill(channel *model.Channel) ([]byte, error) {
	baseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	var url string
	switch channel.Type {
	case constant.ChannelTypeAli:
		url = fmt.Sprintf("%s/compatible-mode/v1/models", baseURL)
	case constant.ChannelTypeZhipu_v4:
		if plan, ok := constant.ChannelSpecialBases[baseURL]; ok && plan.OpenAIBaseURL != "" {
			url = fmt.Sprintf("%s/models", plan.OpenAIBaseURL)
		} else {
			url = fmt.Sprintf("%s/api/paas/v4/models", baseURL)
		}
	case constant.ChannelTypeVolcEngine:
		if plan, ok := constant.ChannelSpecialBases[baseURL]; ok && plan.OpenAIBaseURL != "" {
			url = fmt.Sprintf("%s/v1/models", plan.OpenAIBaseURL)
		} else {
			url = fmt.Sprintf("%s/v1/models", baseURL)
		}
	case constant.ChannelTypeMoonshot:
		if plan, ok := constant.ChannelSpecialBases[baseURL]; ok && plan.OpenAIBaseURL != "" {
			url = fmt.Sprintf("%s/models", plan.OpenAIBaseURL)
		} else {
			url = fmt.Sprintf("%s/v1/models", baseURL)
		}
	default:
		url = fmt.Sprintf("%s/v1/models", baseURL)
	}

	key, _, apiErr := channel.GetNextEnabledKey()
	if apiErr != nil {
		return nil, fmt.Errorf("获取渠道密钥失败: %w", apiErr)
	}
	key = strings.TrimSpace(key)

	headers, err := buildFetchModelsHeaders(channel, key)
	if err != nil {
		return nil, err
	}

	return GetResponseBody(http.MethodGet, url, channel, headers)
}

func fetchOllamaModelForPrefill(channel *model.Channel, modelName string) (*ollama.OllamaModel, error) {
	baseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}
	key := strings.TrimSpace(strings.Split(channel.Key, "\n")[0])
	items, err := ollama.FetchOllamaModels(baseURL, key)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].Name == modelName {
			return &items[i], nil
		}
	}
	return nil, fmt.Errorf("渠道 %d 的 Ollama 模型列表中未找到模型 %s", channel.Id, modelName)
}

func parseOllamaModelCreated(value string) int64 {
	if value == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0
	}
	return t.Unix()
}

func normalizeOllamaQuantization(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	switch {
	case lower == "":
		return ""
	case strings.Contains(lower, "q4"), strings.Contains(lower, "int4"):
		return "int4"
	case strings.Contains(lower, "q8"), strings.Contains(lower, "int8"):
		return "int8"
	case strings.Contains(lower, "bf16"):
		return "bf16"
	case strings.Contains(lower, "fp16"):
		return "fp16"
	case strings.Contains(lower, "fp32"):
		return "fp32"
	default:
		return ""
	}
}

func toOpenRouterModel(item *model.OpenRouterModelMetaWithModel) dto.OpenRouterModel {
	out := dto.OpenRouterModel{
		Id:              item.ModelName,
		HuggingFaceId:   item.HuggingFaceId,
		Name:            pickString(item.Name, item.ModelName),
		Created:         item.Created,
		Description:     pickString(item.Description, item.ModelDescription),
		ContextLength:   item.ContextLength,
		MaxOutputLength: item.MaxOutputLength,
		Quantization:    item.Quantization,
		DeprecationDate: item.DeprecationDate,
	}
	if v := parseStringArray(item.InputModalities); len(v) > 0 {
		out.InputModalities = v
	}
	if v := parseStringArray(item.OutputModalities); len(v) > 0 {
		out.OutputModalities = v
	}
	if v := parseStringArray(item.SupportedSamplingParameters); len(v) > 0 {
		out.SupportedSamplingParameters = v
	}
	if v := parseStringArray(item.SupportedFeatures); len(v) > 0 {
		out.SupportedFeatures = v
	}
	if dcs := parseDatacenters(item.Datacenters); len(dcs) > 0 {
		out.Datacenters = dcs
	}
	if item.Pricing != "" && isJSON(item.Pricing) {
		out.Pricing = json.RawMessage(item.Pricing)
	}
	if item.OpenRouterSlug != "" {
		out.OpenRouter = &dto.OpenRouterSlug{Slug: item.OpenRouterSlug}
	}
	return out
}

// validateOpenRouterModelMetaJSON 校验管理端提交的 JSON 字段是否合法。
func validateOpenRouterModelMetaJSON(m *model.OpenRouterModelMeta) error {
	checks := []struct {
		name  string
		value string
	}{
		{"input_modalities", m.InputModalities},
		{"output_modalities", m.OutputModalities},
		{"supported_sampling_parameters", m.SupportedSamplingParameters},
		{"supported_features", m.SupportedFeatures},
		{"datacenters", m.Datacenters},
		{"pricing", m.Pricing},
	}
	for _, ch := range checks {
		if ch.value == "" {
			continue
		}
		if !isJSON(ch.value) {
			return fmt.Errorf("字段 %s 不是合法 JSON", ch.name)
		}
	}
	return nil
}

func isJSON(s string) bool {
	var v interface{}
	return common.UnmarshalJsonStr(s, &v) == nil
}

func pickString(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func parseStringArray(s string) []string {
	if s == "" {
		return nil
	}
	var arr []string
	if err := common.UnmarshalJsonStr(s, &arr); err != nil {
		return nil
	}
	return arr
}

func parseDatacenters(s string) []dto.OpenRouterDatacenter {
	if s == "" {
		return nil
	}
	var arr []dto.OpenRouterDatacenter
	if err := common.UnmarshalJsonStr(s, &arr); err != nil {
		return nil
	}
	return arr
}
