package model

import (
	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// OpenRouterModelMeta 存放 OpenRouter "List Models" 接口所需的模型元数据。
// 由于这些字段对上游 NewAPI 的 Model 表来说是外部契约的一部分，
// 采用独立表存储以避免与上游代码冲突。
type OpenRouterModelMeta struct {
	Id                          int            `json:"id"`
	ModelId                     int            `json:"model_id" gorm:"not null;uniqueIndex:uk_or_model_meta_model_id_delete_at,priority:1"`
	HuggingFaceId               string         `json:"hugging_face_id" gorm:"size:256"`
	Name                        string         `json:"name" gorm:"size:256"`
	Created                     int64          `json:"created" gorm:"bigint"`
	Description                 string         `json:"description,omitempty" gorm:"type:text"`
	ContextLength               int            `json:"context_length"`
	MaxOutputLength             int            `json:"max_output_length"`
	InputModalities             string         `json:"input_modalities" gorm:"type:text"`
	OutputModalities            string         `json:"output_modalities" gorm:"type:text"`
	Quantization                string         `json:"quantization" gorm:"size:32"`
	Pricing                     string         `json:"pricing" gorm:"type:text"`
	SupportedSamplingParameters string         `json:"supported_sampling_parameters" gorm:"type:text"`
	SupportedFeatures           string         `json:"supported_features" gorm:"type:text"`
	DeprecationDate             string         `json:"deprecation_date,omitempty" gorm:"size:64"`
	OpenRouterSlug              string         `json:"openrouter_slug" gorm:"size:256"`
	Datacenters                 string         `json:"datacenters" gorm:"type:text"`
	CreatedTime                 int64          `json:"created_time" gorm:"bigint"`
	UpdatedTime                 int64          `json:"updated_time" gorm:"bigint"`
	DeletedAt                   gorm.DeletedAt `json:"-" gorm:"index;uniqueIndex:uk_or_model_meta_model_id_delete_at,priority:2"`
}

// OpenRouterModelMetaWithModel 管理后台视图：元数据 + Model 基础信息。
type OpenRouterModelMetaWithModel struct {
	OpenRouterModelMeta
	ModelName        string `json:"model_name"`
	ModelDescription string `json:"model_description"`
	ModelIcon        string `json:"model_icon"`
	ModelStatus      int    `json:"model_status"`
}

type OpenRouterModelBoundChannel struct {
	Id      int    `json:"id"`
	Name    string `json:"name"`
	Type    int    `json:"type"`
	BaseURL string `json:"base_url"`
}

func (m *OpenRouterModelMeta) Insert() error {
	now := common.GetTimestamp()
	m.CreatedTime = now
	m.UpdatedTime = now
	return DB.Create(m).Error
}

func (m *OpenRouterModelMeta) Update() error {
	m.UpdatedTime = common.GetTimestamp()
	return DB.Save(m).Error
}

func (m *OpenRouterModelMeta) Delete() error {
	return DB.Delete(m).Error
}

func GetOpenRouterModelMetaByID(id int) (*OpenRouterModelMetaWithModel, error) {
	var m OpenRouterModelMeta
	if err := DB.First(&m, id).Error; err != nil {
		return nil, err
	}
	result := &OpenRouterModelMetaWithModel{OpenRouterModelMeta: m}
	var mm Model
	if err := DB.First(&mm, m.ModelId).Error; err == nil {
		result.ModelName = mm.ModelName
		result.ModelDescription = mm.Description
		result.ModelIcon = mm.Icon
		result.ModelStatus = mm.Status
	}
	return result, nil
}

func GetOpenRouterModelBoundChannels(modelName string) ([]OpenRouterModelBoundChannel, error) {
	if modelName == "" {
		return []OpenRouterModelBoundChannel{}, nil
	}

	var channels []OpenRouterModelBoundChannel
	err := DB.Table("channels").
		Select("DISTINCT channels.id, channels.name, channels.type, COALESCE(channels.base_url, '') as base_url").
		Joins("JOIN abilities ON abilities.channel_id = channels.id").
		Where("abilities.model = ? AND abilities.enabled = ? AND channels.status = ?", modelName, true, 1).
		Order("channels.priority DESC, channels.id DESC").
		Scan(&channels).Error
	if err != nil {
		return nil, err
	}
	return channels, nil
}

// IsOpenRouterModelMetaDuplicated 同一 ModelId 只能存在一条元数据。
func IsOpenRouterModelMetaDuplicated(id int, modelId int) (bool, error) {
	if modelId == 0 {
		return false, nil
	}
	var cnt int64
	err := DB.Model(&OpenRouterModelMeta{}).Where("model_id = ? AND id <> ?", modelId, id).Count(&cnt).Error
	return cnt > 0, err
}

// GetAllOpenRouterModelMetas 分页获取元数据，并附带 Model 基本信息。
func GetAllOpenRouterModelMetas(keyword string, offset int, limit int) ([]*OpenRouterModelMetaWithModel, int64, error) {
	db := DB.Model(&OpenRouterModelMeta{})
	if keyword != "" {
		like := "%" + keyword + "%"
		var modelIDs []int
		if err := DB.Model(&Model{}).Where("model_name LIKE ? OR description LIKE ?", like, like).Pluck("id", &modelIDs).Error; err != nil {
			return nil, 0, err
		}
		if len(modelIDs) > 0 {
			db = db.Where("name LIKE ? OR hugging_face_id LIKE ? OR openrouter_slug LIKE ? OR model_id IN ?", like, like, like, modelIDs)
		} else {
			db = db.Where("name LIKE ? OR hugging_face_id LIKE ? OR openrouter_slug LIKE ?", like, like, like)
		}
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var metas []OpenRouterModelMeta
	if err := db.Offset(offset).Limit(limit).Order("id DESC").Find(&metas).Error; err != nil {
		return nil, 0, err
	}
	if len(metas) == 0 {
		return []*OpenRouterModelMetaWithModel{}, total, nil
	}

	modelIDs := make([]int, 0, len(metas))
	for _, m := range metas {
		modelIDs = append(modelIDs, m.ModelId)
	}
	var models []Model
	if err := DB.Where("id IN ?", modelIDs).Find(&models).Error; err != nil {
		return nil, 0, err
	}
	modelMap := make(map[int]Model, len(models))
	for _, m := range models {
		modelMap[m.Id] = m
	}

	result := make([]*OpenRouterModelMetaWithModel, 0, len(metas))
	for _, meta := range metas {
		item := &OpenRouterModelMetaWithModel{OpenRouterModelMeta: meta}
		if mm, ok := modelMap[meta.ModelId]; ok {
			item.ModelName = mm.ModelName
			item.ModelDescription = mm.Description
			item.ModelIcon = mm.Icon
			item.ModelStatus = mm.Status
		}
		result = append(result, item)
	}
	return result, total, nil
}

// GetEnabledOpenRouterModels 返回全部带 OpenRouter 元数据且模型已启用的记录，
// 供对外 List Models 接口使用。
func GetEnabledOpenRouterModels() ([]*OpenRouterModelMetaWithModel, error) {
	var metas []OpenRouterModelMeta
	if err := DB.Find(&metas).Error; err != nil {
		return nil, err
	}
	if len(metas) == 0 {
		return []*OpenRouterModelMetaWithModel{}, nil
	}
	modelIDs := make([]int, 0, len(metas))
	for _, m := range metas {
		modelIDs = append(modelIDs, m.ModelId)
	}
	var models []Model
	if err := DB.Where("id IN ? AND status = ?", modelIDs, 1).Find(&models).Error; err != nil {
		return nil, err
	}
	modelMap := make(map[int]Model, len(models))
	for _, m := range models {
		modelMap[m.Id] = m
	}
	result := make([]*OpenRouterModelMetaWithModel, 0, len(metas))
	for _, meta := range metas {
		mm, ok := modelMap[meta.ModelId]
		if !ok {
			continue
		}
		item := &OpenRouterModelMetaWithModel{OpenRouterModelMeta: meta}
		item.ModelName = mm.ModelName
		item.ModelDescription = mm.Description
		item.ModelIcon = mm.Icon
		item.ModelStatus = mm.Status
		result = append(result, item)
	}
	return result, nil
}
