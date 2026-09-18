package mozia_setting

import (
	"errors"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

// Restrict paths to object keys and array indices; no queries, modifiers or scripts.
var routeFieldPath = regexp.MustCompile(`^[A-Za-z0-9_-]+(\.[A-Za-z0-9_-]+)*$`)

func ValidateRouteCondition(condition RouteCondition) error {
	if condition.Operator == "has_video" {
		if condition.Path != "" || len(condition.Value) != 0 {
			return errors.New("has_video does not accept a path or value")
		}
		return nil
	}
	if len(condition.Path) > 256 || !routeFieldPath.MatchString(condition.Path) {
		return errors.New("condition path must contain only object keys and array indices")
	}
	switch condition.Operator {
	case "exists":
		if len(condition.Value) != 0 {
			return errors.New("exists does not accept a value")
		}
	case "equals":
		if len(condition.Value) > 1024 || !gjson.ValidBytes(condition.Value) || gjson.ParseBytes(condition.Value).IsObject() || gjson.ParseBytes(condition.Value).IsArray() {
			return errors.New("equals requires a JSON string, number, boolean or null")
		}
	default:
		return errors.New("unsupported routing condition")
	}
	return nil
}

func (condition RouteCondition) Matches(body []byte) bool {
	if condition.Operator == "has_video" {
		return HasVideoInput(body)
	}
	actual := gjson.GetBytes(body, condition.Path)
	if !actual.Exists() {
		return false
	}
	if condition.Operator == "exists" {
		return true
	}
	expected := gjson.ParseBytes(condition.Value)
	if condition.Operator != "equals" || actual.Type != expected.Type {
		return false
	}
	if actual.Type == gjson.Number {
		for _, raw := range []string{actual.Raw, expected.Raw} {
			if len(raw) > 128 {
				return false
			}
			if index := strings.IndexAny(raw, "eE"); index >= 0 {
				exponent, err := strconv.Atoi(raw[index+1:])
				if err != nil || exponent < -1000 || exponent > 1000 {
					return false
				}
			}
		}
		a, okA := new(big.Rat).SetString(actual.Raw)
		b, okB := new(big.Rat).SetString(expected.Raw)
		return okA && okB && a.Cmp(b) == 0
	}
	return actual.String() == expected.String()
}

// HasVideoInput checks structured media blocks, never URLs or words in prompt text.
// File IDs without media metadata cannot identify a video and are not inferred.
func HasVideoInput(body []byte) bool {
	root := gjson.ParseBytes(body)
	for _, path := range []string{"messages", "input", "contents"} {
		for _, message := range root.Get(path).Array() {
			parts := message.Get("content").Array()
			if path == "contents" {
				parts = message.Get("parts").Array()
			}
			for _, part := range parts {
				switch part.Get("type").String() {
				case "video_url", "input_video", "video":
					return true
				}
				for _, mimePath := range []string{"source.media_type", "mime_type", "file.mime_type", "fileData.mimeType", "inlineData.mimeType", "file_data.mime_type", "inline_data.mime_type"} {
					if strings.HasPrefix(strings.ToLower(part.Get(mimePath).String()), "video/") {
						return true
					}
				}
			}
		}
	}
	return false
}

func MatchUserModelRedirect(userID int, modelName, endpoint, thinkingType string, body []byte) (UserModelRedirect, bool) {
	// ponytail: scan at most 1000 rules; index by source model if routing volume warrants it.
	for _, rule := range GetUserModelRedirects() {
		if rule.Disabled || (!rule.AllUsers && rule.UserId != userID) || rule.SourceModel != modelName ||
			(rule.Endpoint != "" && rule.Endpoint != endpoint) || (rule.OnlyThinkingDisabled && thinkingType != "disabled") {
			continue
		}
		matched := true
		for _, condition := range rule.Conditions {
			if !condition.Matches(body) {
				matched = false
				break
			}
		}
		if matched {
			return rule, true
		}
	}
	return UserModelRedirect{}, false
}
