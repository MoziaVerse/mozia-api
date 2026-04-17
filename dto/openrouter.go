package dto

import "encoding/json"

// OpenRouterListModelsResponse 对外响应结构，遵循 OpenRouter Providers
// "List Models" 规范：https://openrouter.ai/docs/guides/get-started/for-providers
type OpenRouterListModelsResponse struct {
	Data []OpenRouterModel `json:"data"`
}

// OpenRouterModel 单个模型条目。
// pricing 允许是 object（标准）或 array（分层定价），使用 RawMessage 透传。
type OpenRouterModel struct {
	Id                          string                 `json:"id"`
	HuggingFaceId               string                 `json:"hugging_face_id,omitempty"`
	Name                        string                 `json:"name,omitempty"`
	Created                     int64                  `json:"created,omitempty"`
	Description                 string                 `json:"description,omitempty"`
	ContextLength               int                    `json:"context_length,omitempty"`
	MaxOutputLength             int                    `json:"max_output_length,omitempty"`
	InputModalities             []string               `json:"input_modalities,omitempty"`
	OutputModalities            []string               `json:"output_modalities,omitempty"`
	Quantization                string                 `json:"quantization,omitempty"`
	Pricing                     json.RawMessage        `json:"pricing,omitempty"`
	SupportedSamplingParameters []string               `json:"supported_sampling_parameters,omitempty"`
	SupportedFeatures           []string               `json:"supported_features,omitempty"`
	DeprecationDate             string                 `json:"deprecation_date,omitempty"`
	OpenRouter                  *OpenRouterSlug        `json:"openrouter,omitempty"`
	Datacenters                 []OpenRouterDatacenter `json:"datacenters,omitempty"`
}

type OpenRouterSlug struct {
	Slug string `json:"slug"`
}

type OpenRouterDatacenter struct {
	CountryCode string `json:"country_code"`
}
