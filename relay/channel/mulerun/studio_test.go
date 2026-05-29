package mulerun

import (
	"testing"

	taskmulerun "github.com/QuantumNous/new-api/relay/channel/task/mulerun"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
)

// taskmulerunStudioIDs 缓存 task 包 registry 的全部 CLI ID，避免每个用例
// 重复构造（小函数包一层是为了让上面 TestModelListIsChatPlusStudioUnion
// 阅读时知道这是别人维护的集合，不在本包断言里现编）。
func taskmulerunStudioIDs(t *testing.T) []string {
	t.Helper()
	return taskmulerun.StudioModelIDs()
}

// GetRequestURL 必须根据 RelayFormat / RelayMode 选对 chat / messages / 其他
// 协议的 upstream 路径，并且不被 Base URL 的前后空白或末尾斜杠影响。
//
// 注：mulerun studio 多模态端点不再走本 adaptor，已迁移到
// relay/channel/task/mulerun/TaskAdaptor，覆盖其行为的测试也在那里。
func TestAdaptorGetRequestURL(t *testing.T) {
	base := "https://api.mulerun.com"

	tests := []struct {
		name string
		info *relaycommon.RelayInfo
		want string
	}{
		{
			name: "chat completions (OpenAI)",
			info: &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: base},
				RelayMode:   constant.RelayModeChatCompletions,
				RelayFormat: types.RelayFormatOpenAI,
			},
			want: "https://api.mulerun.com/v1/chat/completions",
		},
		{
			name: "completions (legacy)",
			info: &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: base},
				RelayMode:   constant.RelayModeCompletions,
				RelayFormat: types.RelayFormatOpenAI,
			},
			want: "https://api.mulerun.com/v1/completions",
		},
		{
			name: "embeddings",
			info: &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: base},
				RelayMode:   constant.RelayModeEmbeddings,
				RelayFormat: types.RelayFormatOpenAI,
			},
			want: "https://api.mulerun.com/v1/embeddings",
		},
		{
			name: "anthropic messages",
			info: &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: base},
				RelayFormat: types.RelayFormatClaude,
			},
			want: "https://api.mulerun.com/v1/messages",
		},
		{
			name: "base url 前后空白与末尾 / 都被剥离",
			info: &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "  " + base + "/  "},
				RelayMode:   constant.RelayModeChatCompletions,
				RelayFormat: types.RelayFormatOpenAI,
			},
			want: "https://api.mulerun.com/v1/chat/completions",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := &Adaptor{}
			got, err := a.GetRequestURL(tc.info)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("GetRequestURL=%q, want %q", got, tc.want)
			}
		})
	}
}

// ModelList 应当：
// - 完整包含 ChatModelList 的全部 chat 模型；
// - 完整包含 task 包注册表里所有 studio 端点（来自 mulerun CLI 抓取）；
// - 不引入第三类模型（避免悄悄混入未注册条目）。
//
// 这样创建 Mulerun 渠道时管理后台一份多选框既能勾 chat 也能勾 studio，
// 而路由层按 URL（/v1/chat/completions vs /v1/video/generations）自动分派
// 到对应 adaptor。
func TestModelListIsChatPlusStudioUnion(t *testing.T) {
	set := make(map[string]struct{}, len(ModelList))
	for _, m := range ModelList {
		set[m] = struct{}{}
	}

	// 全部 chat 模型必须在
	for _, m := range ChatModelList {
		if _, ok := set[m]; !ok {
			t.Errorf("ModelList 缺 chat model %q", m)
		}
	}

	// 全部 studio 端点必须在
	for _, id := range taskmulerunStudioIDs(t) {
		if _, ok := set[id]; !ok {
			t.Errorf("ModelList 缺 studio model %q", id)
		}
	}

	// 总数严格等于两者之和（不应有多余）
	if want := len(ChatModelList) + len(taskmulerunStudioIDs(t)); len(ModelList) != want {
		t.Errorf("ModelList 长度 = %d，期望 %d (chat=%d + studio=%d)",
			len(ModelList), want, len(ChatModelList), len(taskmulerunStudioIDs(t)))
	}
}
