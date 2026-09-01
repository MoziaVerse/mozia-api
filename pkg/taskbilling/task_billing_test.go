package taskbilling

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluatePerSecondUsesEffectiveDuration(t *testing.T) {
	config := Config{
		Version: Version1,
		Mode:    ModePerSecond,
		Duration: &Dimension{
			Paths: []string{"duration", "seconds", "metadata.duration"},
			Round: RoundCeil,
			Unit:  1,
		},
	}

	ratios, err := Evaluate(config, []byte(`{"seconds":"4.2"}`))
	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"duration": 5}, ratios)
}

func TestEvaluateParametricUsesDefaultsAndEnumMultipliers(t *testing.T) {
	config := Config{
		Version: Version1,
		Mode:    ModeParametric,
		Dimensions: []Dimension{
			{
				Name:    "duration",
				Kind:    DimensionNumber,
				Paths:   []string{"duration", "metadata.duration"},
				Default: 5,
				Round:   RoundCeil,
			},
			{
				Name:    "resolution",
				Kind:    DimensionEnum,
				Paths:   []string{"resolution", "metadata.resolution"},
				Default: "720p",
				Values: map[string]float64{
					"480p": 1,
					"720p": 2.15,
				},
			},
		},
	}

	ratios, err := Evaluate(config, []byte(`{"duration":8,"resolution":"480P"}`))
	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"duration": 8, "resolution": 1}, ratios)

	ratios, err = Evaluate(config, []byte(`{}`))
	require.NoError(t, err)
	assert.Equal(t, map[string]float64{"duration": 5, "resolution": 2.15}, ratios)
}

func TestEvaluateTokenPricingSelectsResolutionAndTrustedReferenceState(t *testing.T) {
	reference480p := 21.0
	reference1080p := 23.25
	config := Config{
		Version: Version1,
		Mode:    ModeTokenParametric,
		TokenPrices: &TokenPriceTable{
			Paths: []string{"resolution", "metadata.resolution"},
			Values: map[string]TokenUnitPrice{
				"480p":  {Standard: 34.5, ReferenceVideo: &reference480p},
				"1080p": {Standard: 38.25, ReferenceVideo: &reference1080p},
			},
		},
	}

	standard, err := EvaluateTokenPricing(config, []byte(`{"resolution":"1080P"}`), false)
	require.NoError(t, err)
	require.NotNil(t, standard.TokenPrice)
	assert.Equal(t, &TokenPriceResult{Resolution: "1080p", UnitPrice: 38.25}, standard.TokenPrice)

	reference, err := EvaluateTokenPricing(config, []byte(`{"resolution":"480p"}`), true)
	require.NoError(t, err)
	require.NotNil(t, reference.TokenPrice)
	assert.Equal(t, &TokenPriceResult{Resolution: "480p", ReferenceVideo: true, UnitPrice: 21}, reference.TokenPrice)

	_, err = EvaluateTokenPricing(config, []byte(`{"resolution":"720p"}`), false)
	assert.ErrorContains(t, err, "does not support resolution")
}

func TestValidateRejectsAmbiguousOrInvalidTaskBilling(t *testing.T) {
	tests := []Config{
		{Version: Version1, Mode: ModePerSecond},
		{Version: Version1, Mode: ModePerRequest, Dimensions: []Dimension{{}}},
		{Version: Version1, Mode: ModeParametric, Dimensions: []Dimension{{Name: "mode", Kind: DimensionEnum, Paths: []string{"mode"}}}},
		{Version: 2, Mode: ModePerRequest},
		{Version: Version1, Mode: ModeParametric, Dimensions: []Dimension{{Name: "quality", Kind: DimensionEnum, Paths: []string{"quality"}, Default: "high", Values: map[string]float64{"low": 1}}}},
		{Version: Version1, Mode: ModePerRequest, Surcharge: &Surcharge{Name: "images", Kind: SurchargeItemCount, Paths: []string{"images"}, UnitPrice: -0.2}},
		{Version: Version1, Mode: ModeTokenParametric},
	}

	for _, config := range tests {
		assert.Error(t, Validate(config))
	}
}

func TestEvaluatePricingCountsBillableImages(t *testing.T) {
	config := Config{
		Version: Version1,
		Mode:    ModePerSecond,
		Duration: &Dimension{
			Paths: []string{"duration"},
			Round: RoundCeil,
		},
		Surcharge: &Surcharge{
			Name:      "input_images",
			Kind:      SurchargeItemCount,
			Paths:     []string{"conditions", "metadata.conditions", "content", "images", "image", "input_reference"},
			ItemTypes: []string{"image", "image_url"},
			FreeCount: 5,
			UnitPrice: 0.2,
		},
	}

	tests := []struct {
		name          string
		body          string
		count         int
		billableCount int
		price         float64
	}{
		{name: "five images are free", body: `{"duration":15,"images":["1","2","3","4","5"]}`, count: 5},
		{name: "string image list", body: `{"duration":15,"images":["1","2","3","4","5","6"]}`, count: 6, billableCount: 1, price: 0.2},
		{name: "condition types are filtered", body: `{"duration":15,"conditions":[{"type":"image"},{"type":"video"},{"type":"image"},{"type":"audio"},{"type":"image"},{"type":"image"},{"type":"image"},{"type":"image"}]}`, count: 6, billableCount: 1, price: 0.2},
		{name: "content image urls", body: `{"duration":15,"content":[{"type":"text"},{"type":"image_url"},{"type":"image_url"},{"type":"image_url"},{"type":"image_url"},{"type":"image_url"},{"type":"image_url"}]}`, count: 6, billableCount: 1, price: 0.2},
		{name: "first present source wins", body: `{"duration":15,"conditions":[{"type":"image"}],"images":["1","2","3","4","5","6"]}`, count: 1},
		{name: "empty sources are skipped", body: `{"duration":15,"conditions":[],"content":[],"images":["1","2","3","4","5","6"]}`, count: 6, billableCount: 1, price: 0.2},
		{name: "single image is counted", body: `{"duration":15,"image":"https://example.com/image.png"}`, count: 1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			evaluation, err := EvaluatePricing(config, []byte(test.body))
			require.NoError(t, err)
			assert.Equal(t, map[string]float64{"duration": 15}, evaluation.Ratios)
			require.NotNil(t, evaluation.Surcharge)
			assert.Equal(t, test.count, evaluation.Surcharge.Count)
			assert.Equal(t, test.billableCount, evaluation.Surcharge.BillableCount)
			assert.InDelta(t, test.price, evaluation.Surcharge.Price, 0.000001)
		})
	}
}

func TestEvaluatePricingRejectsObjectSurchargeInput(t *testing.T) {
	config := Config{
		Version: Version1,
		Mode:    ModePerRequest,
		Surcharge: &Surcharge{
			Name: "images", Kind: SurchargeItemCount, Paths: []string{"images"}, UnitPrice: 0.2,
		},
	}

	_, err := EvaluatePricing(config, []byte(`{"images":{"url":"not-an-array"}}`))
	assert.Error(t, err)
}
