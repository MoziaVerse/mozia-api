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

func TestValidateRejectsAmbiguousOrInvalidTaskBilling(t *testing.T) {
	tests := []Config{
		{Version: Version1, Mode: ModePerSecond},
		{Version: Version1, Mode: ModePerRequest, Dimensions: []Dimension{{}}},
		{Version: Version1, Mode: ModeParametric, Dimensions: []Dimension{{Name: "mode", Kind: DimensionEnum, Paths: []string{"mode"}}}},
		{Version: 2, Mode: ModePerRequest},
		{Version: Version1, Mode: ModeParametric, Dimensions: []Dimension{{Name: "quality", Kind: DimensionEnum, Paths: []string{"quality"}, Default: "high", Values: map[string]float64{"low": 1}}}},
	}

	for _, config := range tests {
		assert.Error(t, Validate(config))
	}
}
