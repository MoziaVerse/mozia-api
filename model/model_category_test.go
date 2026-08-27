package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModelCategoryFromTags(t *testing.T) {
	tests := []struct {
		name         string
		tags         string
		wantCategory string
		wantError    string
	}{
		{name: "reads category", tags: " 视频, CATEGORY:Video ,图生视频 ", wantCategory: "video"},
		{name: "allows legacy tags", tags: "对话,推理"},
		{name: "rejects unknown category", tags: "category:other", wantError: "invalid model category"},
		{name: "rejects multiple categories", tags: "category:text,category:video", wantError: "exactly one"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCategory, err := ModelCategoryFromTags(tt.tags)
			if tt.wantError != "" {
				assert.ErrorContains(t, err, tt.wantError)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantCategory, gotCategory)
		})
	}
}

func TestRequiredModelCategoryFromTagsRejectsMissingCategory(t *testing.T) {
	_, err := RequiredModelCategoryFromTags("推理,工具调用")
	assert.ErrorContains(t, err, "exactly one")
}
