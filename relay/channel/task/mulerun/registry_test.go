package mulerun

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// ShortKey 必须：
//   - 长度恒为 8（不能超出 task.Action 列的 varchar(40) 限制）
//   - 稳定（同 cli_id 多次调用产同样结果）
//   - 在 43 条 endpoint 间无冲突（hash collision 检查）
//
// 这条测试是质量底线——一旦冲突或长度漂移就会出现「轮询 endpoint 串台」
// 或「DB 静默 truncate 导致 GET 报 task_not_exist」。
func TestShortKey(t *testing.T) {
	const wantLen = 8

	seen := make(map[string]string, len(StudioEndpoints))
	for _, ep := range StudioEndpoints {
		key := ShortKey(ep.CLIID)
		if len(key) != wantLen {
			t.Errorf("ShortKey(%q) 长度 = %d, want %d", ep.CLIID, len(key), wantLen)
		}
		if prev, dup := seen[key]; dup {
			t.Errorf("ShortKey 冲突：%q 与 %q 都映射到 %q", ep.CLIID, prev, key)
		}
		seen[key] = ep.CLIID

		// 稳定性：同一 cli_id 调两次必须相等
		if again := ShortKey(ep.CLIID); again != key {
			t.Errorf("ShortKey(%q) 不稳定: %q vs %q", ep.CLIID, key, again)
		}
	}
}

// LookupStudioByShortKey 是 polling 阶段重建 APIPath 的入口；
// 这里固定几个关键 endpoint 验证 cli_id → ShortKey → endpoint 三向一致。
func TestLookupStudioByShortKey(t *testing.T) {
	for _, ep := range StudioEndpoints {
		key := ShortKey(ep.CLIID)
		got := LookupStudioByShortKey(key)
		if got == nil {
			t.Errorf("LookupStudioByShortKey(%q) = nil, cli_id=%q", key, ep.CLIID)
			continue
		}
		if got.CLIID != ep.CLIID {
			t.Errorf("LookupStudioByShortKey(%q) → cli_id=%q, want %q", key, got.CLIID, ep.CLIID)
		}
		if got.APIPath != ep.APIPath {
			t.Errorf("LookupStudioByShortKey(%q) → apiPath=%q, want %q", key, got.APIPath, ep.APIPath)
		}
	}

	// 空字符串与未知 key 必须返回 nil（不能匹配到任意一条）
	if got := LookupStudioByShortKey(""); got != nil {
		t.Errorf("LookupStudioByShortKey(\"\") 应返回 nil，got %+v", got)
	}
	if got := LookupStudioByShortKey("deadbeef"); got != nil {
		// "deadbeef" 不在 43 条 endpoint 的 hash 集合里
		t.Errorf("LookupStudioByShortKey(\"deadbeef\") 应返回 nil，got %+v", got)
	}
}

// ShortKey 必须 fit task.Action 的 varchar(40)，
// 留余量验证：8 字符远小于 40，没什么意外余地，但显式断言可读性更好。
func TestShortKeyFitsTaskActionColumn(t *testing.T) {
	const taskActionLimit = 40
	for _, ep := range StudioEndpoints {
		key := ShortKey(ep.CLIID)
		if len(key) > taskActionLimit {
			t.Fatalf("ShortKey(%q)=%q 长度 %d 超过 task.Action 列 varchar(%d) 限制",
				ep.CLIID, key, len(key), taskActionLimit)
		}
	}
}

func TestFirstResultURL(t *testing.T) {
	tests := []struct {
		name string
		in   taskResponse
		want string
	}{
		{
			name: "mulerun seedance returns video url as string item",
			in: taskResponse{
				Videos: []interface{}{"https://mule-router-assets.example/seedance_result_00.mp4"},
			},
			want: "https://mule-router-assets.example/seedance_result_00.mp4",
		},
		{
			name: "object item with url remains supported",
			in: taskResponse{
				Images: []interface{}{map[string]interface{}{"url": "https://example.test/image.png"}},
			},
			want: "https://example.test/image.png",
		},
		{
			name: "object item with uri is accepted",
			in: taskResponse{
				Audios: []interface{}{map[string]interface{}{"uri": "https://example.test/audio.mp3"}},
			},
			want: "https://example.test/audio.mp3",
		},
		{
			name: "empty results",
			in:   taskResponse{},
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstResultURL(tc.in); got != tc.want {
				t.Fatalf("firstResultURL()=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestPrepareBodyForEndpointStripsGatewayModel(t *testing.T) {
	ep := LookupStudioEndpoint("bytedance/seedance-2.0-fast/text-to-video")
	got, err := prepareBodyForEndpoint([]byte(`{"model":"bytedance/seedance-2.0-fast/text-to-video","prompt":"hello","duration":4}`), ep)
	if err != nil {
		t.Fatal(err)
	}

	var body map[string]any
	if err := common.Unmarshal(got, &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["model"]; ok {
		t.Fatalf("model should be stripped for URL-routed endpoints, got body=%s", got)
	}
	if body["prompt"] != "hello" {
		t.Fatalf("prompt should be preserved, got body=%s", got)
	}
}

func TestPrepareBodyForEndpointAddsVeoModelVariant(t *testing.T) {
	ep := LookupStudioEndpoint("google/veo3/generation")
	got, err := prepareBodyForEndpoint([]byte(`{"model":"google/veo3/generation","prompt":"hello","duration":4}`), ep)
	if err != nil {
		t.Fatal(err)
	}

	var body map[string]any
	if err := common.Unmarshal(got, &body); err != nil {
		t.Fatal(err)
	}
	if body["model"] != "veo-3.1" {
		t.Fatalf("Veo endpoint should receive upstream model variant, got body=%s", got)
	}
	if body["prompt"] != "hello" {
		t.Fatalf("prompt should be preserved, got body=%s", got)
	}
}

func TestConvertToOpenAIVideoSanitizesFailureMessage(t *testing.T) {
	originTask := &model.Task{
		TaskID: "video_public_123",
		Status: model.TaskStatusFailure,
		Data:   []byte(`{"task_info":{"status":"failed","error":"MuleRun Studio failed at https://api.mulerun.com/v1/tasks/abc"}}`),
	}

	got, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(originTask)
	if err != nil {
		t.Fatal(err)
	}

	var video dto.OpenAIVideo
	if err := common.Unmarshal(got, &video); err != nil {
		t.Fatal(err)
	}
	if video.Error == nil {
		t.Fatal("expected failed task response to include error")
	}
	msg := strings.ToLower(video.Error.Message)
	for _, forbidden := range []string{"mulerun", "studio", "api.mulerun.com"} {
		if strings.Contains(msg, forbidden) {
			t.Fatalf("video error leaks %q: %q", forbidden, video.Error.Message)
		}
	}
	if !strings.Contains(msg, "upstream") {
		t.Fatalf("video error should keep a generic upstream hint, got %q", video.Error.Message)
	}
}
