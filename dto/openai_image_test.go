package dto

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageRequestPreservesSGLangDiffusionParameters(t *testing.T) {
	input := []byte(`{
		"model":"Qwen-Image-2512",
		"prompt":"test image",
		"num_inference_steps":0,
		"guidance_scale":0,
		"true_cfg_scale":0,
		"seed":0,
		"negative_prompt":""
	}`)

	var request ImageRequest
	require.NoError(t, common.Unmarshal(input, &request))
	require.NotNil(t, request.NumInferenceSteps)
	require.NotNil(t, request.GuidanceScale)
	require.NotNil(t, request.TrueCfgScale)
	require.NotNil(t, request.Seed)
	require.NotNil(t, request.NegativePrompt)
	assert.Zero(t, *request.NumInferenceSteps)
	assert.Zero(t, *request.GuidanceScale)
	assert.Zero(t, *request.TrueCfgScale)
	assert.Zero(t, *request.Seed)
	assert.Empty(t, *request.NegativePrompt)

	encoded, err := common.Marshal(request)
	require.NoError(t, err)

	var output map[string]any
	require.NoError(t, common.Unmarshal(encoded, &output))
	assert.Equal(t, float64(0), output["num_inference_steps"])
	assert.Equal(t, float64(0), output["guidance_scale"])
	assert.Equal(t, float64(0), output["true_cfg_scale"])
	assert.Equal(t, float64(0), output["seed"])
	assert.Equal(t, "", output["negative_prompt"])
}

func TestImageRequestOmitsAbsentSGLangDiffusionParameters(t *testing.T) {
	request := ImageRequest{Model: "Qwen-Image-2512", Prompt: "test image"}

	encoded, err := common.Marshal(request)
	require.NoError(t, err)

	var output map[string]any
	require.NoError(t, common.Unmarshal(encoded, &output))
	assert.NotContains(t, output, "num_inference_steps")
	assert.NotContains(t, output, "guidance_scale")
	assert.NotContains(t, output, "true_cfg_scale")
	assert.NotContains(t, output, "seed")
	assert.NotContains(t, output, "negative_prompt")
}
