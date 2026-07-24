package globalaiopc

import (
	"errors"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// Adaptor 适配 GlobalAiOpc Videos 系列异步视频渠道。
//
// 当前默认视频服务基址为 https://zcbservice.aizfw.cn/kyyReactApiServer，
// 该地址已核实用于 Videos 异步视频任务。非视频任务接口会在
// 本地拒绝，不会被误转发到上游。
//
// 默认视频任务由 TaskAdaptor 处理：
//
//	POST /v1/videos/videos
//	GET  /v1/result/{task_id}
var errTaskOnly = errors.New("Globalaiopc only supports video task endpoints; use /v1/videos or /v1/video/generations")

type Adaptor struct{}

func (a *Adaptor) Init(_ *relaycommon.RelayInfo) {}
func (a *Adaptor) GetRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return "", errTaskOnly
}
func (a *Adaptor) SetupRequestHeader(_ *gin.Context, _ *http.Header, _ *relaycommon.RelayInfo) error {
	return errTaskOnly
}
func (a *Adaptor) ConvertOpenAIRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ *dto.GeneralOpenAIRequest) (any, error) {
	return nil, errTaskOnly
}
func (a *Adaptor) ConvertClaudeRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ *dto.ClaudeRequest) (any, error) {
	return nil, errTaskOnly
}
func (a *Adaptor) ConvertGeminiRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ *dto.GeminiChatRequest) (any, error) {
	return nil, errTaskOnly
}
func (a *Adaptor) ConvertEmbeddingRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ dto.EmbeddingRequest) (any, error) {
	return nil, errTaskOnly
}
func (a *Adaptor) ConvertRerankRequest(_ *gin.Context, _ int, _ dto.RerankRequest) (any, error) {
	return nil, errTaskOnly
}
func (a *Adaptor) ConvertAudioRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ dto.AudioRequest) (io.Reader, error) {
	return nil, errTaskOnly
}
func (a *Adaptor) ConvertImageRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ dto.ImageRequest) (any, error) {
	return nil, errTaskOnly
}
func (a *Adaptor) ConvertOpenAIResponsesRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ dto.OpenAIResponsesRequest) (any, error) {
	return nil, errTaskOnly
}
func (a *Adaptor) DoRequest(_ *gin.Context, _ *relaycommon.RelayInfo, _ io.Reader) (any, error) {
	return nil, errTaskOnly
}
func (a *Adaptor) DoResponse(_ *gin.Context, _ *http.Response, _ *relaycommon.RelayInfo) (any, *types.NewAPIError) {
	return nil, types.NewErrorWithStatusCode(errTaskOnly, types.ErrorCodeInvalidRequest, http.StatusBadRequest)
}

func (a *Adaptor) GetModelList() []string { return ModelList }
func (a *Adaptor) GetChannelName() string { return ChannelName }
