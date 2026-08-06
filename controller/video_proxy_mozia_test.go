package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
)

func TestResolveMoziaVideoContentURL(t *testing.T) {
	tests := []struct {
		name        string
		channelType int
		baseURL     string
		resultURL   string
		wantURL     string
		wantAuth    bool
	}{
		{
			name:        "seedance content fallback",
			channelType: constant.ChannelTypeMoziaSeedanceGen,
			baseURL:     "https://provider.example",
			wantURL:     "https://provider.example/v1/video/generations/upstream-task/content",
			wantAuth:    true,
		},
		{
			name:        "videos content fallback without duplicate v1",
			channelType: constant.ChannelTypeMoziaSeedanceVideos,
			baseURL:     "https://provider.example/v1/",
			resultURL:   "https://gateway.example/v1/videos/task_public/content",
			wantURL:     "https://provider.example/v1/videos/upstream-task/content",
			wantAuth:    true,
		},
		{
			name:        "seedance public alias does not recurse",
			channelType: constant.ChannelTypeMoziaSeedanceGen,
			baseURL:     "https://provider.example/v1",
			resultURL:   "https://gateway.example/v1/video/generations/task_public/content",
			wantURL:     "https://provider.example/v1/video/generations/upstream-task/content",
			wantAuth:    true,
		},
		{
			name:        "direct CDN URL stays unauthenticated",
			channelType: constant.ChannelTypeMoziaSeedanceVideos,
			baseURL:     "https://provider.example",
			resultURL:   "https://cdn.example/result.mp4",
			wantURL:     "https://cdn.example/result.mp4",
			wantAuth:    false,
		},
		{
			name:        "empty provider base URL does not use OpenAI fallback",
			channelType: constant.ChannelTypeMoziaSeedanceVideos,
			resultURL:   "https://gateway.example/v1/videos/task_public/content",
			wantURL:     "",
			wantAuth:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			task := &model.Task{
				TaskID: "task_public",
				PrivateData: model.TaskPrivateData{
					UpstreamTaskID: "upstream-task",
					ResultURL:      tc.resultURL,
				},
			}

			gotURL, gotAuth := resolveMoziaVideoContentURL(tc.channelType, tc.baseURL, task)

			assert.Equal(t, tc.wantURL, gotURL)
			assert.Equal(t, tc.wantAuth, gotAuth)
		})
	}
}
