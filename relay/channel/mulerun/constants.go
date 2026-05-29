package mulerun

import taskmulerun "github.com/QuantumNous/new-api/relay/channel/task/mulerun"

// mulerun（MuleRun）多模型网关。
// 文档：https://mule.mintlify.app/cli/quickstart
// CLI：`mulerun code -m <model>` 实际通过 https://api.mulerun.com 转发。
//
// 同一 ChannelType (constant.ChannelTypeMoziaMulerun) 承载两类调用：
//
//   1. chat / messages 模型 —— 走本包 Adaptor，路径 /v1/chat/completions 或
//      /v1/messages，OpenAI / Anthropic 双协议。
//
//   2. studio 多模态模型（image / video / audio / music）—— 走 task 子系统，
//      adaptor 在 relay/channel/task/mulerun/，路由对外暴露为 OpenAI Sora 标准
//      /v1/video/generations + GET /v1/video/generations/:task_id，
//      由 new-api 负责 polling、计费、状态持久化。
//
// 鉴权统一使用 `Authorization: Bearer muk-...`。

// ChatModelList 仅包含 chat / messages 模型。
var ChatModelList = []string{
	"openai/gpt-5.5",
	"openai/gpt-5.4-mini",
	"openai/gpt-5.4-nano",
	"openai/gpt-5.3-codex",
	"google/gemini-3.1-pro-preview",
	"google/gemini-3.1-flash-lite-preview",
	"google/gemini-3-pro-preview",
	"google/gemini-3-flash-preview",
	"alibaba/qwen3.6-max-preview",
	"alibaba/qwen3.6-plus",
	"moonshot/kimi-k2.6",
	"zhipu/glm-5.1",
}

// ModelList 是创建 channel 时暴露给管理后台的模型清单，
// 等于 chat + studio 全集——这样用户在创建/编辑 Mulerun 渠道时
// 一份 model 多选框里既能勾 chat 也能勾 studio，任何模型都路由到本渠道
// （chat 走 chat adaptor、studio 走 task adaptor，由 new-api 路由分派）。
var ModelList = func() []string {
	studio := taskmulerun.StudioModelIDs()
	out := make([]string, 0, len(ChatModelList)+len(studio))
	out = append(out, ChatModelList...)
	out = append(out, studio...)
	return out
}()

var ChannelName = "mulerun"
