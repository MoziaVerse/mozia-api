package mulerun

import (
	"crypto/sha1"
	"encoding/hex"
)

// studio_registry.go —— mulerun studio 模型的权威映射表。
//
// 数据来源：对每个 `mulerun studio list` 列出的 endpoint 跑一次
//   mulerun studio params <cli-id>
// 解析其 "API Path / Output Type / Result Key" 字段生成。
//
// 必须维持「显式注册」而非「字符串拼接」，因为存在三类无法仅靠字符串规则
// 推导出 upstream 路径的情况：
//
//   1. klingai 别名映射：CLI 用 `kling-v3-omni-t2v` 这类短代号，upstream
//      真路径却是 `kling-v3-omni/text-to-video`。
//   2. midjourney 路径重命名：CLI 是 `midjourney/diffusion/generation`，
//      upstream 是 `/vendors/midjourney/v1/tob/diffusion`，多了 `tob` 段、
//      且没有尾部 `/generation`。
//   3. minimax 中段补齐：CLI 是 `minimax/music-2.5/generation`，upstream
//      实际是 `/vendors/minimax/v1/music-2.5/text-to-music/generation`，
//      中间插了一段 `text-to-music`。
//   4. google/veo3 → upstream `/vendors/google/v1/veo/generation`（去掉
//      尾部 "3"）。
//
// 这些都是 mulerun CLI 内部维护的别名表，原 CLI 通过 endpoint registry
// 直接持有，我们的网关只能照样落表。新增 / 变更 endpoint 时，**不要**手改
// 本文件，而是重跑 scripts/refresh_mulerun_studio.sh，让脚本基于最新 CLI
// 输出重新生成。

// StudioEndpoint 描述一个 mulerun studio 端点。
//
// CLIID 是用户在 mulerun CLI（`mulerun studio params <id>`）中使用的模型 ID，
// 也是 mozia 网关 `/v1/video/generations` 请求体里 `model` 字段应当填入
// 的字符串。
type StudioEndpoint struct {
	CLIID      string // mulerun CLI 模型 ID（请求体 model 字段值）
	APIPath    string // upstream 真实路径（不含 BaseURL，以 / 开头）
	OutputType string // image / video / audio
	ResultKey  string // 成功响应里产物数组的字段名：images / videos / audios
}

// StudioEndpoints 全量 43 条 endpoint。按 CLIID 字典序排列以便 diff 友好。
// 顺序无功能意义；查找走 studioByCLIID 索引。
var StudioEndpoints = []StudioEndpoint{
	{CLIID: "alibaba/happy-horse-1-0-i2v/generation", APIPath: "/vendors/alibaba/v1/happy-horse-1-0-i2v/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "alibaba/happy-horse-1-0-t2v/generation", APIPath: "/vendors/alibaba/v1/happy-horse-1-0-t2v/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "alibaba/wan2.1-kf2v-plus/generation", APIPath: "/vendors/alibaba/v1/wan2.1-kf2v-plus/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "alibaba/wan2.1-vace-plus/generation", APIPath: "/vendors/alibaba/v1/wan2.1-vace-plus/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "alibaba/wan2.2-i2v-flash/generation", APIPath: "/vendors/alibaba/v1/wan2.2-i2v-flash/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "alibaba/wan2.2-i2v-plus/generation", APIPath: "/vendors/alibaba/v1/wan2.2-i2v-plus/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "alibaba/wan2.2-t2v-plus/generation", APIPath: "/vendors/alibaba/v1/wan2.2-t2v-plus/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "alibaba/wan2.5-i2i-preview/generation", APIPath: "/vendors/alibaba/v1/wan2.5-i2i-preview/generation", OutputType: "image", ResultKey: "images"},
	{CLIID: "alibaba/wan2.5-i2v-preview/generation", APIPath: "/vendors/alibaba/v1/wan2.5-i2v-preview/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "alibaba/wan2.5-t2i-preview/generation", APIPath: "/vendors/alibaba/v1/wan2.5-t2i-preview/generation", OutputType: "image", ResultKey: "images"},
	{CLIID: "alibaba/wan2.5-t2v-preview/generation", APIPath: "/vendors/alibaba/v1/wan2.5-t2v-preview/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "alibaba/wan2.6-i2v/generation", APIPath: "/vendors/alibaba/v1/wan2.6-i2v/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "alibaba/wan2.6-image/generation", APIPath: "/vendors/alibaba/v1/wan2.6-image/generation", OutputType: "image", ResultKey: "images"},
	{CLIID: "alibaba/wan2.6-t2i/generation", APIPath: "/vendors/alibaba/v1/wan2.6-t2i/generation", OutputType: "image", ResultKey: "images"},
	{CLIID: "alibaba/wan2.6-t2v/generation", APIPath: "/vendors/alibaba/v1/wan2.6-t2v/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "bytedance/seedance-2.0-fast/image-to-video", APIPath: "/vendors/bytedance/v1/seedance-2.0-fast/image-to-video/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "bytedance/seedance-2.0-fast/reference-to-video", APIPath: "/vendors/bytedance/v1/seedance-2.0-fast/reference-to-video/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "bytedance/seedance-2.0-fast/text-to-video", APIPath: "/vendors/bytedance/v1/seedance-2.0-fast/text-to-video/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "bytedance/seedance-2.0/image-to-video", APIPath: "/vendors/bytedance/v1/seedance-2.0/image-to-video/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "bytedance/seedance-2.0/reference-to-video", APIPath: "/vendors/bytedance/v1/seedance-2.0/reference-to-video/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "bytedance/seedance-2.0/text-to-video", APIPath: "/vendors/bytedance/v1/seedance-2.0/text-to-video/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "google/nano-banana-2/edit", APIPath: "/vendors/google/v1/nano-banana-2/edit", OutputType: "image", ResultKey: "images"},
	{CLIID: "google/nano-banana-2/generation", APIPath: "/vendors/google/v1/nano-banana-2/generation", OutputType: "image", ResultKey: "images"},
	{CLIID: "google/nano-banana-pro/edit", APIPath: "/vendors/google/v1/nano-banana-pro/edit", OutputType: "image", ResultKey: "images"},
	{CLIID: "google/nano-banana-pro/generation", APIPath: "/vendors/google/v1/nano-banana-pro/generation", OutputType: "image", ResultKey: "images"},
	{CLIID: "google/nano-banana/edit", APIPath: "/vendors/google/v1/nano-banana/edit", OutputType: "image", ResultKey: "images"},
	{CLIID: "google/nano-banana/generation", APIPath: "/vendors/google/v1/nano-banana/generation", OutputType: "image", ResultKey: "images"},
	{CLIID: "google/veo3/generation", APIPath: "/vendors/google/v1/veo/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "klingai/kling-v3-i2v/generation", APIPath: "/vendors/klingai/v1/kling-v3/image-to-video/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "klingai/kling-v3-omni-i2v/generation", APIPath: "/vendors/klingai/v1/kling-v3-omni/image-to-video/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "klingai/kling-v3-omni-ref2v/generation", APIPath: "/vendors/klingai/v1/kling-v3-omni/reference-image-to-video/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "klingai/kling-v3-omni-t2v/generation", APIPath: "/vendors/klingai/v1/kling-v3-omni/text-to-video/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "klingai/kling-v3-omni-v2v-edit/generation", APIPath: "/vendors/klingai/v1/kling-v3-omni/video-to-video/edit", OutputType: "video", ResultKey: "videos"},
	{CLIID: "klingai/kling-v3-omni-v2v/generation", APIPath: "/vendors/klingai/v1/kling-v3-omni/reference-video-to-video/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "klingai/kling-v3-t2v/generation", APIPath: "/vendors/klingai/v1/kling-v3/text-to-video/generation", OutputType: "video", ResultKey: "videos"},
	{CLIID: "midjourney/diffusion/generation", APIPath: "/vendors/midjourney/v1/tob/diffusion", OutputType: "image", ResultKey: "images"},
	{CLIID: "midjourney/video/generation", APIPath: "/vendors/midjourney/v1/tob/video-diffusion", OutputType: "video", ResultKey: "videos"},
	{CLIID: "minimax/music-2.0/generation", APIPath: "/vendors/minimax/v1/music-2.0/text-to-music/generation", OutputType: "audio", ResultKey: "audios"},
	{CLIID: "minimax/music-2.5/generation", APIPath: "/vendors/minimax/v1/music-2.5/text-to-music/generation", OutputType: "audio", ResultKey: "audios"},
	{CLIID: "minimax/speech-2.8-hd/generation", APIPath: "/vendors/minimax/v1/speech-2.8-hd/text-to-speech/generation", OutputType: "audio", ResultKey: "audios"},
	{CLIID: "minimax/speech-2.8-turbo/generation", APIPath: "/vendors/minimax/v1/speech-2.8-turbo/text-to-speech/generation", OutputType: "audio", ResultKey: "audios"},
	{CLIID: "openai/gpt-image-2/edit", APIPath: "/vendors/openai/v1/gpt-image-2/edit", OutputType: "image", ResultKey: "images"},
	{CLIID: "openai/gpt-image-2/generation", APIPath: "/vendors/openai/v1/gpt-image-2/generation", OutputType: "image", ResultKey: "images"},
}

// studioByCLIID O(1) 按 CLI ID 查 endpoint。
var studioByCLIID = func() map[string]*StudioEndpoint {
	m := make(map[string]*StudioEndpoint, len(StudioEndpoints))
	for i := range StudioEndpoints {
		m[StudioEndpoints[i].CLIID] = &StudioEndpoints[i]
	}
	return m
}()

// studioByShortKey 按 ShortKey（cli_id 的 sha1 前 8 字符 hex）反查 endpoint。
//
// 用途：new-api 的 task 表 `action` 列只有 varchar(40)，无法直接存
// 73 字符的完整 APIPath 或最长 56 字符的 cli_id，否则 Insert 静默失败，
// GET /v1/video/generations/:task_id 报 task_not_exist。
// ShortKey 长度恒定 8 字符，永远 fit varchar(40)；cli_id 不变 hash 就不变，
// refresh registry 重排顺序也不影响已经持久化的 task。
var studioByShortKey = func() map[string]*StudioEndpoint {
	m := make(map[string]*StudioEndpoint, len(StudioEndpoints))
	for i := range StudioEndpoints {
		m[ShortKey(StudioEndpoints[i].CLIID)] = &StudioEndpoints[i]
	}
	return m
}()

// LookupStudioEndpoint 按 CLI ID 查 endpoint。未找到返回 nil。
func LookupStudioEndpoint(cliID string) *StudioEndpoint {
	if cliID == "" {
		return nil
	}
	return studioByCLIID[cliID]
}

// LookupStudioByShortKey 按 ShortKey 反查 endpoint。
// polling worker 在 FetchTask 中收到 task.Action（= ShortKey），
// 通过这里重建完整 APIPath。
func LookupStudioByShortKey(shortKey string) *StudioEndpoint {
	if shortKey == "" {
		return nil
	}
	return studioByShortKey[shortKey]
}

// ShortKey 把 cli_id 压成 8 字符 hex（sha1 前 4 字节）。
// 见 studioByShortKey 注释解释为什么要用 short key 而不是 cli_id / APIPath。
func ShortKey(cliID string) string {
	sum := sha1Sum([]byte(cliID))
	return hexEncode(sum[:4])
}

// sha1Sum / hexEncode 提取出来仅为了便于 fake/替换：实现走标准库。
func sha1Sum(b []byte) [20]byte {
	return sha1.Sum(b)
}

func hexEncode(b []byte) string {
	return hex.EncodeToString(b)
}

// StudioModelIDs 返回所有已注册 CLI ID（用于 constants.ModelList / 测试断言）。
func StudioModelIDs() []string {
	out := make([]string, 0, len(StudioEndpoints))
	for i := range StudioEndpoints {
		out = append(out, StudioEndpoints[i].CLIID)
	}
	return out
}
