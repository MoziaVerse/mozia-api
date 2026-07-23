package common

import "github.com/QuantumNous/new-api/constant"

// Mozia 私有渠道的 APIType 注册表。
//
// 与 ChannelType 同样选择 200+ 高位避免与 upstream 未来 APIType 撞号。
// 通过 MoziaChannelType2APIType + 在 api_type.go 顶部加一行 hook 完成 ChannelType→APIType
// 的非侵入式扩展。
const (
	APITypeMoziaGlobalaiopc = 200
	APITypeMoziaMulerun     = 201
	// Cool 走 TaskAdaptor 路径，不参与 ChannelType→APIType 映射。
)

var moziaChannelToAPIType = map[int]int{
	constant.ChannelTypeMoziaGlobalaiopc: APITypeMoziaGlobalaiopc,
	constant.ChannelTypeMoziaMulerun:     APITypeMoziaMulerun,
}

// MoziaChannelType2APIType 在 ChannelType2APIType 的 switch 前调用，命中则直接返回。
func MoziaChannelType2APIType(channelType int) (int, bool) {
	at, ok := moziaChannelToAPIType[channelType]
	return at, ok
}
