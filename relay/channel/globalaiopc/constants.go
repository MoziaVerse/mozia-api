package globalaiopc

// GlobalAiOpc Seedance2.0 特价版渠道。
//
// 当前默认视频基址为 https://zcbservice.aizfw.cn/kyyReactApiServer ，
// 当前仅适配 Seedance-special 异步视频任务。

// ModelList 仅用于展示常见的官方上游模型，不是路由白名单。渠道实际对外
// 模型及 alias -> upstream 映射始终以渠道的 models / model_mapping 配置为准。

var ModelList = []string{
	"sd_2.0_special_720p",
	"sd_2.0_special_1080p",
	"sd_2.0_special_2k",
	"sd_2.0_special_4k",
	"sd_2.0_special_720p_with_video_ref",
	"sd_2.0_special_1080p_with_video_ref",
	"sd_2.0_special_2k_with_video_ref",
	"sd_2.0_special_4k_with_video_ref",
	"sd_2.0_fast_special_720p",
	"sd_2.0_fast_special_720p_with_video_ref",
}

var ChannelName = "globalaiopc"
