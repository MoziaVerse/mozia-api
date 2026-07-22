package modelrouter

// 阿里教育 Model Router 网关。
// 控制台：https://model-router-console.edu-aliyun.com/open-api
// 协议：同步接口兼容 OpenAI；异步视频使用 ModelRouter 的 DashScope 风格任务协议。
//
// 实际可用模型由控制台分发的 API Key 决定；这里放一份示例列表，使用者
// 必须在创建渠道时按自己 Key 实际订阅的模型改写 models 字段。

var ModelList = []string{
	// chat
	"deepseek-v3", "deepseek-r1", "qwen-max", "qwen-plus", "qwen-turbo",
	"gpt-4o", "gpt-4o-mini", "claude-3-5-sonnet",
	// image
	"dall-e-3", "gpt-image-1",
	// video
	"sora-2", "seedance-2.0", "seedance-2.0-fast",
	// tts / audio
	"tts-1", "whisper-1",
	// embedding
	"text-embedding-3-small", "text-embedding-3-large",
}

var ChannelName = "modelrouter"
