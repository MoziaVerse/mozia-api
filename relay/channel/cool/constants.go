package cool

// Cool API 网关（异步图片/视频生成）。
// 文档：飞书 wiki ABAAw89K3ioqBhkonHNczfyHn9d
// Base URL：https://api.mjapi.cc.cd
//
// 流程：POST /v1/cool/generate 提交 → 返回 task_id → 轮询 GET /v1/cool/task/{task_id}
// 鉴权：Authorization: Bearer <key>  或  X-API-Key: <key>

const (
	defaultImageModel = "gpt_image_2"
	defaultVideoModel = "seedance_2_fast"
)

// ImageModels 是按张计费的图片模型集合。
var ImageModels = []string{
	"gpt_image_2",
	"seedream_4_5",
	"midjourney_v7",
	"flux_kontext_pro",
	"nano_banana_2",
	"nano_banana_pro",
}

// VideoModels 是按次计费（单次定额）的视频模型集合。
// Seedance 2.0 系列按「分辨率 × 时长」动态计费，单独放在 SeedanceSKUs。
var VideoModels = []string{
	"seedance_1_5_pro_audio",
	"seedance_1_5_pro_silent",
	"happyhorse_1",
	"kling_3_silent",
	"kling_3_omni",
	"kling_3_audio",
	"vidu_q3",
	"vidu_q2_pro",
	"vidu_q3_pro",
	"wan_2_6",
	"r_sd2",
}

// SeedanceSKUs 是对外暴露的 Seedance 2.0 计费 SKU（带 cool: 前缀）。
//
// 模型名编码了三要素：基础模型(seedance_2 / seedance_2_fast) + 分辨率 + 是否参考视频，
// 由 billing.go 的 parseSeedanceModel 解析回 cool 上游真实 model key + resolution 参数：
//
//   - cool:seedance_2 / cool:seedance_2_fast        无分辨率后缀 → 动态路由，默认 720p
//   - cool:seedance_2_480p / _720p / _1080p          固定分辨率（后缀优先，忽略请求体）
//   - cool:seedance_2_*_video                        参考视频生成（files 带 type:video），价格更高
//
// cool 官方 /v1/cool/generate 的 model 字段只认基础 key（seedance_2 / seedance_2_fast），
// 分辨率走独立的 resolution 字段；参考视频不是单独模型，而是 files 里带 type:video 的素材。
// 价格见 https://yuanman.mjapi.cc.cd/api/pricing（按秒计费）。fast 版本无 1080p。
var SeedanceSKUs = []string{
	"cool:seedance_2",
	"cool:seedance_2_480p",
	"cool:seedance_2_720p",
	"cool:seedance_2_1080p",
	"cool:seedance_2_480p_video",
	"cool:seedance_2_720p_video",
	"cool:seedance_2_1080p_video",
	"cool:seedance_2_fast",
	"cool:seedance_2_fast_480p",
	"cool:seedance_2_fast_720p",
	"cool:seedance_2_fast_480p_video",
	"cool:seedance_2_fast_720p_video",
}

var ModelList = append(append(append([]string{}, ImageModels...), VideoModels...), SeedanceSKUs...)

var ChannelName = "cool"

func isVideoModel(model string) bool {
	for _, m := range VideoModels {
		if m == model {
			return true
		}
	}
	return false
}
