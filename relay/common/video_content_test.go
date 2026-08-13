package common

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	apicommon "github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskSubmitReqParseVideoContentFrames(t *testing.T) {
	for _, tt := range []struct {
		name       string
		body       string
		wantPrompt string
		wantImages []string
		wantLast   string
	}{
		{
			name:       "first and last frames",
			body:       `{"content":[{"type":"text","text":"make it cinematic"},{"type":"image_url","role":"first_frame","image_url":{"url":"https://example.com/first.png"}},{"type":"image_url","role":"last_frame","image_url":{"url":"https://example.com/last.png"}}]}`,
			wantPrompt: "make it cinematic",
			wantImages: []string{"https://example.com/first.png", "https://example.com/last.png"},
			wantLast:   "https://example.com/last.png",
		},
		{
			name:       "first frame only is valid",
			body:       `{"prompt":"keep prompt","content":[{"type":"image_url","role":"first_frame","image_url":{"url":"https://example.com/first.png"}}]}`,
			wantPrompt: "keep prompt",
			wantImages: []string{"https://example.com/first.png"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var req TaskSubmitReq
			require.NoError(t, apicommon.Unmarshal([]byte(tt.body), &req))

			summary, err := req.ParseVideoContent()
			require.NoError(t, err)
			assert.Equal(t, tt.wantPrompt, summary.Prompt)
			assert.Equal(t, tt.wantImages, summary.LegacyImages())
			assert.Equal(t, tt.wantLast, summary.LastFrameURL)
		})
	}
}

func TestTaskSubmitReqParseVideoContentReferences(t *testing.T) {
	var req TaskSubmitReq
	body := `{"content":[{"type":"text","text":"keep the subject"},{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/ref.png"}},{"type":"video_url","role":"reference_video","video_url":{"url":"https://example.com/ref.mp4"}},{"type":"audio_url","role":"reference_audio","audio_url":{"url":"https://example.com/ref.mp3"}}]}`
	require.NoError(t, apicommon.Unmarshal([]byte(body), &req))

	summary, err := req.ParseVideoContent()
	require.NoError(t, err)
	assert.Equal(t, "keep the subject", summary.Prompt)
	assert.Equal(t, []string{"https://example.com/ref.png"}, summary.ReferenceImages)
	assert.Equal(t, []string{"https://example.com/ref.mp4"}, summary.ReferenceVideos)
	assert.Equal(t, []string{"https://example.com/ref.mp3"}, summary.ReferenceAudios)
	assert.Equal(t, []string{"https://example.com/ref.png"}, summary.LegacyImages())
}

func TestTaskSubmitReqParseVideoContentRejectsFrameReferenceMixing(t *testing.T) {
	var req TaskSubmitReq
	body := `{"content":[{"type":"text","text":"mixing"},{"type":"image_url","role":"first_frame","image_url":{"url":"https://example.com/first.png"}},{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/ref.png"}}]}`
	require.NoError(t, apicommon.Unmarshal([]byte(body), &req))

	_, err := req.ParseVideoContent()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be mixed")
}

func TestTaskSubmitReqParseVideoContentRejectsSecondCanonicalTextPrompt(t *testing.T) {
	var req TaskSubmitReq
	body := `{"content":[{"type":"text","text":"first prompt"},{"type":"text","text":"second prompt"}]}`
	require.NoError(t, apicommon.Unmarshal([]byte(body), &req))

	_, err := req.ParseVideoContent()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "second text prompt")
}

func TestValidateBasicTaskRequestPrefersTopLevelPromptAndBackfillsImages(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := `{"prompt":"legacy prompt","content":[{"type":"text","text":"new prompt"},{"type":"image_url","role":"first_frame","image_url":{"url":"https://example.com/first.png"}},{"type":"image_url","role":"last_frame","image_url":{"url":"https://example.com/last.png"}}]}`
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", bytes.NewReader([]byte(body)))
	ctx.Request.Header.Set("Content-Type", "application/json")

	storage, err := apicommon.GetBodyStorage(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = storage.Close() })

	info := &RelayInfo{TaskRelayInfo: &TaskRelayInfo{}}
	taskErr := ValidateBasicTaskRequest(ctx, info, "generate")
	require.Nil(t, taskErr)

	req, err := GetTaskRequest(ctx)
	require.NoError(t, err)
	assert.Equal(t, "legacy prompt", req.Prompt)
	assert.Equal(t, []string{"https://example.com/first.png", "https://example.com/last.png"}, req.Images)
	assert.Len(t, req.Content, 3)
}
