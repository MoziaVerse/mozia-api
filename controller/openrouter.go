package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

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
