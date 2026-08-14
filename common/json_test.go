package common

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJsonRawMessageToString(t *testing.T) {
	tests := []struct {
		name string
		data json.RawMessage
		want string
	}{
		{
			name: "object",
			data: json.RawMessage(`{"city":"Paris","days":0,"strict":false}`),
			want: `{"city":"Paris","days":0,"strict":false}`,
		},
		{
			name: "string",
			data: json.RawMessage(`"{\"city\":\"Paris\",\"days\":0,\"strict\":false}"`),
			want: `{"city":"Paris","days":0,"strict":false}`,
		},
		{
			name: "null",
			data: json.RawMessage(`null`),
			want: "",
		},
		{
			name: "empty",
			data: nil,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, JsonRawMessageToString(tt.data))
		})
	}
}

func TestMarshalNoEscapeHTML(t *testing.T) {
	body, err := MarshalNoEscapeHTML(struct {
		URL     string `json:"url"`
		Literal string `json:"literal"`
	}{
		URL:     "https://example.com/video.mp4?expires=1&signature=test&uid=2",
		Literal: `\u0026`,
	})
	require.NoError(t, err)
	require.Equal(t, `{"url":"https://example.com/video.mp4?expires=1&signature=test&uid=2","literal":"\\u0026"}`, string(body))
}
