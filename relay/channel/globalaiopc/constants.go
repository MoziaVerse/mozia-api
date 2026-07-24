package globalaiopc

// GlobalAiOpc Videos 异步视频渠道。
//
// 当前默认视频基址为 https://zcbservice.aizfw.cn/kyyReactApiServer ，
// 当前适配 /v1/videos/videos 提交与 /v1/result/{id} 查询。

// ModelList 是 Videos 创建接口当前支持的官方上游模型。渠道实际对外模型
// 仍由 models 配置，公开别名通过 model_mapping 映射到这些模型。

var ModelList = []string{
	"videos",
	"videos_stable",
	"videos_stable_fast",
	"videos_pro",
	"videos_pro_fast",
}

var ChannelName = "globalaiopc"
