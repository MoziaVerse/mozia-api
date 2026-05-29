package mulerun

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	"github.com/QuantumNous/new-api/relay/channel/openai"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// Adaptor 适配 mulerun 网关的 chat / messages 协议（同时支持两套）：
//
//   - RelayFormatOpenAI  → POST {base}/v1/chat/completions
//   - RelayFormatClaude  → POST {base}/v1/messages（需 anthropic-version）
//
// chat 与 messages 的请求/响应形态完全等同 openai.Adaptor / claude.Adaptor，
// 这里只做 URL/Header 定制 + base URL 净化。
//
// 注：mulerun studio 多模态端点（image / video / audio / music）走 task 子系统，
// 由 relay/channel/task/mulerun/TaskAdaptor 处理，对外暴露 OpenAI Sora 标准
// /v1/video/generations 接口，不在本文件之内。
type Adaptor struct{}

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	// 后台粘贴的 Base URL 经常带前/后空格或末尾斜杠，统一归一化一次，
	// 避免 net/url.Parse 报 "first path segment in URL cannot contain colon"。
	base := strings.TrimRight(strings.TrimSpace(info.ChannelBaseUrl), "/")
	switch info.RelayFormat {
	case types.RelayFormatClaude:
		return fmt.Sprintf("%s/v1/messages", base), nil
	default:
		switch info.RelayMode {
		case constant.RelayModeChatCompletions:
			return fmt.Sprintf("%s/v1/chat/completions", base), nil
		case constant.RelayModeCompletions:
			return fmt.Sprintf("%s/v1/completions", base), nil
		case constant.RelayModeEmbeddings:
			return fmt.Sprintf("%s/v1/embeddings", base), nil
		default:
			return fmt.Sprintf("%s/v1/chat/completions", base), nil
		}
	}
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	channel.SetupApiRequestHeader(info, c, req)
	req.Set("Authorization", "Bearer "+info.ApiKey)
	if info.RelayFormat == types.RelayFormatClaude {
		// Anthropic 协议必带版本头；mulerun 后端目前接受 2023-06-01 起的所有版本。
		if req.Get("anthropic-version") == "" {
			req.Set("anthropic-version", "2023-06-01")
		}
	}
	return nil
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	return request, nil
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, req *dto.ClaudeRequest) (any, error) {
	adaptor := claude.Adaptor{}
	return adaptor.ConvertClaudeRequest(c, info, req)
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return request, nil
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return request, nil
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("not supported")
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, errors.New("not supported (mulerun studio 多模态走 /v1/video/generations)")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	return nil, errors.New("not implemented")
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	switch info.RelayFormat {
	case types.RelayFormatClaude:
		adaptor := claude.Adaptor{}
		return adaptor.DoResponse(c, resp, info)
	default:
		adaptor := openai.Adaptor{}
		return adaptor.DoResponse(c, resp, info)
	}
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
