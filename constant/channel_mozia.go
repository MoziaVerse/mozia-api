package constant

// Mozia 私有渠道扩展。
//
// 这些渠道是 mozia 分支自有的，不在 upstream new-api 中存在。为减少未来与
// upstream 合并的冲突：
//   - ID 选择 200+ 高位段，留足空间给 upstream 继续在 58、59、60... 增加新
//     渠道，不会与我们的 ID 撞号。
//   - 渠道名、base URL 通过本文件 init() 注入到 upstream 的 ChannelTypeNames
//     map 与 ChannelBaseURLs slice，不修改 upstream 文件本身。
//   - 新增渠道时只往本文件追加常量与 mapping，不动 channel.go。
const (
	ChannelTypeMoziaGlobalaiopc    = 200 // Globalaiopc Videos 系列异步视频
	ChannelTypeMoziaMulerun        = 201 // mulerun 多模型网关（OpenAI + Anthropic 双协议）
	ChannelTypeMoziaCool           = 202 // Cool 图片/视频生成（异步任务）
	ChannelTypeMoziaSeedanceGen    = 203 // Seedance 兼容异步视频渠道（/v1/video/generations）
	ChannelTypeMoziaSeedanceVideos = 204 // Seedance 兼容异步视频渠道（/v1/videos）
)

// moziaChannelTypeNames 与 upstream ChannelTypeNames 同语义；通过 init() 合并。
var moziaChannelTypeNames = map[int]string{
	ChannelTypeMoziaGlobalaiopc:    "Globalaiopc",
	ChannelTypeMoziaMulerun:        "Mulerun",
	ChannelTypeMoziaCool:           "Cool",
	ChannelTypeMoziaSeedanceGen:    "SeedanceCompatible-Gen",
	ChannelTypeMoziaSeedanceVideos: "SeedanceCompatible-Videos",
}

// moziaChannelBaseURLs 与 upstream ChannelBaseURLs slice 同语义；通过 init()
// 把 slice 扩容到能容纳最大 ID，再按 index 写入。
var moziaChannelBaseURLs = map[int]string{
	ChannelTypeMoziaGlobalaiopc:    "https://zcbservice.aizfw.cn/kyyReactApiServer",
	ChannelTypeMoziaMulerun:        "https://api.mulerun.com",
	ChannelTypeMoziaCool:           "https://api.mjapi.cc.cd",
	ChannelTypeMoziaSeedanceGen:    "",
	ChannelTypeMoziaSeedanceVideos: "",
}

func init() {
	maxID := 0
	for id := range moziaChannelBaseURLs {
		if id > maxID {
			maxID = id
		}
	}
	for len(ChannelBaseURLs) <= maxID {
		ChannelBaseURLs = append(ChannelBaseURLs, "")
	}
	for id, url := range moziaChannelBaseURLs {
		ChannelBaseURLs[id] = url
	}
	for id, name := range moziaChannelTypeNames {
		ChannelTypeNames[id] = name
	}
}
