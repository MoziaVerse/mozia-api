package mozia_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConditionalRoutingMatchesOriginalRequest(t *testing.T) {
	original := UserModelRedirects2JSONString()
	t.Cleanup(func() { require.NoError(t, UpdateUserModelRedirectsByJSONString(original)) })
	require.NoError(t, UpdateUserModelRedirectsByJSONString(`{
		"video":{"id":"video","all_users":true,"source_model":"k3","target_model":"k3","target_channel_id":2,"priority":100,"endpoint":"/v1/chat/completions","conditions":[{"operator":"has_video"},{"operator":"equals","path":"stream","value":true}]},
		"customer":{"id":"customer","user_id":7,"source_model":"k3","target_model":"k2","only_thinking_disabled":true},
		"off":{"id":"off","all_users":true,"source_model":"k3","target_model":"off","priority":200,"disabled":true,"only_thinking_disabled":false},
		"chain":{"id":"chain","user_id":7,"source_model":"k2","target_model":"k1","only_thinking_disabled":false}
	}`))
	for _, tc := range []struct {
		name                       string
		user                       int
		path, thinking, body, want string
	}{
		{"video wins over thinking downgrade", 7, "/v1/chat/completions", "disabled", `{"stream":true,"messages":[{"content":[{"type":"video_url","video_url":{"url":"https://example.com/a.mp4"}}]}]}`, "video"},
		{"all users", 8, "/v1/chat/completions", "", `{"stream":true,"messages":[{"content":[{"type":"video_url","video_url":"x"}]}]}`, "video"},
		{"all conditions must match", 7, "/v1/chat/completions", "disabled", `{"stream":false,"messages":[{"content":[{"type":"video_url"}]}]}`, "customer"},
		{"legacy thinking", 7, "/v1/chat/completions", "disabled", `{}`, "customer"},
		{"endpoint does not match", 8, "/v1/messages", "", `{"messages":[{"content":[{"type":"video_url"}]}]}`, ""},
		{"disabled and other user do not match", 8, "/v1/chat/completions", "disabled", `{}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rule, found := MatchUserModelRedirect(tc.user, "k3", tc.path, tc.thinking, []byte(tc.body))
			assert.Equal(t, tc.want != "", found)
			assert.Equal(t, tc.want, rule.ID)
		})
	}
	value, err := BuildUserModelRedirectUpsertJSON(UserModelRedirect{ID: "another", UserId: 7, SourceModel: "k3", TargetModel: "k4", Conditions: []RouteCondition{{Operator: "exists", Path: "tools.0"}}})
	require.NoError(t, err)
	require.NoError(t, UpdateUserModelRedirectsByJSONString(value))
	assert.Len(t, GetUserModelRedirects(), 5)
	value, err = BuildUserModelRedirectDeleteJSON(7, "k3", "another")
	require.NoError(t, err)
	require.NoError(t, UpdateUserModelRedirectsByJSONString(value))
	assert.Len(t, GetUserModelRedirects(), 4)
}

func TestRouteConditionsPreserveJSONTypesAndVideoHistory(t *testing.T) {
	for _, tc := range []struct {
		name      string
		condition RouteCondition
		body      string
		want      bool
	}{
		{"explicit false", RouteCondition{Operator: "equals", Path: "stream", Value: []byte(`false`)}, `{"stream":false}`, true},
		{"missing is not false", RouteCondition{Operator: "equals", Path: "stream", Value: []byte(`false`)}, `{}`, false},
		{"string is not boolean", RouteCondition{Operator: "equals", Path: "stream", Value: []byte(`false`)}, `{"stream":"false"}`, false},
		{"explicit zero", RouteCondition{Operator: "equals", Path: "temperature", Value: []byte(`0`)}, `{"temperature":0.0}`, true},
		{"null exists", RouteCondition{Operator: "exists", Path: "seed"}, `{"seed":null}`, true},
		{"exact large integer", RouteCondition{Operator: "equals", Path: "seed", Value: []byte(`9007199254740992`)}, `{"seed":9007199254740993}`, false},
		{"bounded number parsing", RouteCondition{Operator: "equals", Path: "seed", Value: []byte(`1`)}, `{"seed":1e999999999}`, false},
		{"array index", RouteCondition{Operator: "exists", Path: "tools.0"}, `{"tools":[{}]}`, true},
		{"video in history", RouteCondition{Operator: "has_video"}, `{"messages":[{"content":[{"type":"video_url","video_url":"data:video/mp4;base64,AA=="}]},{"content":"continue"}]}`, true},
		{"responses video", RouteCondition{Operator: "has_video"}, `{"input":[{"content":[{"type":"input_video","video_url":"x"}]}]}`, true},
		{"Gemini video", RouteCondition{Operator: "has_video"}, `{"contents":[{"parts":[{"fileData":{"mimeType":"video/mp4","fileUri":"x"}}]}]}`, true},
		{"text URL is not video", RouteCondition{Operator: "has_video"}, `{"messages":[{"content":"watch https://example.com/a.mp4"}]}`, false},
		{"tool schema is not video", RouteCondition{Operator: "has_video"}, `{"tools":[{"type":"video_url"}],"messages":[{"content":[{"type":"text","text":"video_url"}]}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, ValidateRouteCondition(tc.condition))
			assert.Equal(t, tc.want, tc.condition.Matches([]byte(tc.body)))
		})
	}
	for _, condition := range []RouteCondition{{Operator: "script", Path: "x"}, {Operator: "equals", Path: "x", Value: []byte(`{}`)}, {Operator: "exists", Path: "messages.#(role=\"user\")"}, {Operator: "has_video", Path: "text"}} {
		assert.Error(t, ValidateRouteCondition(condition))
	}
}
