package model

import (
	"fmt"
	"strings"
)

const ModelCategoryTagPrefix = "category:"

var validModelCategories = map[string]struct{}{
	"text":       {},
	"multimodal": {},
	"image":      {},
	"video":      {},
	"audio":      {},
	"embedding":  {},
	"rerank":     {},
}

func ModelCategoryFromTags(tags string) (string, error) {
	category := ""
	for _, part := range strings.Split(tags, ",") {
		tag := strings.TrimSpace(part)
		if tag == "" {
			continue
		}
		lower := strings.ToLower(tag)
		if !strings.HasPrefix(lower, ModelCategoryTagPrefix) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(lower, ModelCategoryTagPrefix))
		if _, ok := validModelCategories[value]; !ok {
			return "", fmt.Errorf("invalid model category %q", value)
		}
		if category != "" {
			return "", fmt.Errorf("model tags must contain exactly one category tag")
		}
		category = value
	}
	return category, nil
}

func RequiredModelCategoryFromTags(tags string) (string, error) {
	category, err := ModelCategoryFromTags(tags)
	if err != nil {
		return "", err
	}
	if category == "" {
		return "", fmt.Errorf("model tags must contain exactly one category tag")
	}
	return category, nil
}
