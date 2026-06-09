package cool

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

// Cool Seedance 2.0 系列按「分辨率 × 时长」动态计费（cool 上游按秒计费）。
//
// 注意：这里的倍率只决定【对客户的扣费】，与 cool 给我们的成本无关，因此取自
// 对外【售价】曲线（mzsjai 定价，元/秒；本部署 RMB:积分=1:1，直接按元填）。
//
// 对外有两类 SKU（见 constants.SeedanceSKUs，由 parseSeedanceModel 解析）：
//
//  1. 动态 SKU：cool:seedance_2 / cool:seedance_2_fast —— 无分辨率后缀，
//     分辨率由请求参数决定（默认 720p）。管理员给【客户调用的模型名】填 720p 售价：
//     doubao/seedance-2.0       => 0.994 / 秒（720p）
//     doubao/seedance-2.0-fast  => 0.800 / 秒（720p）
//     再由 resolution 倍率（相对 720p）+ duration 倍率（=秒数）自动放缩出 480p/1080p。
//
//  2. 固定 SKU：cool:seedance_2_480p / _720p / _1080p 及其 _video 变体 ——
//     分辨率（及是否参考视频）已写死在模型名里，各有独立的每秒售价。管理员
//     把 ModelPrice 直接填该 SKU 的每秒售价，代码只叠加 duration 倍率
//     （=秒数），不再叠加 resolution 倍率（否则会重复计）。
//
// 倍率公式：最终额度 = 每秒单价 × (resolutionRatio?) × durationRatio
//   - durationRatio = duration / seedanceBillingUnitSeconds = duration（按秒线性）
//   - resolutionRatio 仅动态 SKU 生效，取自【售价】曲线（相对 720p）：
//     seedance_2:      480p 0.462/0.994 | 720p 1.0 | 1080p 2.478/0.994
//     seedance_2_fast: 480p 0.372/0.800 | 720p 1.0 |（无 1080p，回落 720p）
//
// 固定 _video SKU 与非 video SKU 走同一 cool 基础模型，差异仅在 files 是否带
// type:video 的参考素材；价格差异由各自的 ModelPrice 体现。
// 非 Seedance 2.0 模型返回 nil，走默认定额计费。
const (
	seedanceBillingUnitSeconds = 1 // cool 按秒计费：ModelPrice 即每秒单价
	seedanceDefaultDuration    = 5 // cool 默认生成 5 秒
	seedanceDefaultResolution  = "720p"
)

var (
	// seedanceStandardResolutionRatios 是 seedance_2 动态 SKU 的清晰度倍率（相对 720p=1.0）。
	// 取自对外【售价】曲线（元/秒）：480p 0.462 | 720p 0.994 | 1080p 2.478。
	seedanceStandardResolutionRatios = map[string]float64{
		"480p":  0.462 / 0.994,
		"720p":  1.0,
		"1080p": 2.478 / 0.994,
	}
	// seedanceFastResolutionRatios 是 seedance_2_fast 动态 SKU 的清晰度倍率（相对 720p=1.0）。
	// 售价曲线（元/秒）：480p 0.372 | 720p 0.800。fast 不支持 1080p：请求 1080p 回落 720p 计费。
	seedanceFastResolutionRatios = map[string]float64{
		"480p": 0.372 / 0.800,
		"720p": 1.0,
	}
)

// seedanceModelSpec 是 Seedance SKU 模型名解析结果。
type seedanceModelSpec struct {
	Base       string // 发给 cool 的真实 model key：seedance_2 / seedance_2_fast
	Resolution string // 480p / 720p / 1080p；动态 SKU 为空
	Video      bool   // _video 后缀：参考视频生成 SKU
	Dynamic    bool   // 无分辨率后缀，按请求参数动态路由（默认 720p）
	OK         bool   // 是否为受支持的 Seedance 2.0 SKU
}

// parseSeedanceModel 把对外 SKU 名（可带 cool: 前缀、分辨率后缀、_video 后缀）
// 解析回 cool 上游基础模型 + 分辨率。非 Seedance 2.0 模型返回 OK=false。
func parseSeedanceModel(model string) seedanceModelSpec {
	m := stripCoolPrefix(model)
	spec := seedanceModelSpec{}
	if strings.HasSuffix(m, "_video") {
		spec.Video = true
		m = strings.TrimSuffix(m, "_video")
	}
	for _, res := range []string{"1080p", "720p", "480p"} {
		if strings.HasSuffix(m, "_"+res) {
			spec.Resolution = res
			m = strings.TrimSuffix(m, "_"+res)
			break
		}
	}
	switch m {
	case "seedance_2", "seedance_2_fast":
		spec.Base = m
		spec.OK = true
	default:
		return seedanceModelSpec{}
	}
	spec.Dynamic = spec.Resolution == ""
	return spec
}

// EstimateBilling 对 Seedance 2.0 SKU 返回计费倍率，在预扣费阶段
// （relay_task.RelayTaskSubmit 第 5 步）被调用；其它 cool 模型返回 nil。
//
//   - 所有 SKU：duration 倍率（按秒线性缩放）。
//   - 仅动态 SKU：额外叠加 resolution 倍率（固定 SKU 的分辨率已体现在其单价里）。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	spec := parseSeedanceModel(coolModelName(info))
	if !spec.OK {
		return nil
	}

	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}

	resolution := spec.Resolution
	if spec.Dynamic {
		resolution = resolveSeedanceResolution(c, &req)
	}
	return seedanceRatios(spec, resolution, resolveSeedanceDuration(c, &req))
}

// seedanceRatios 是计费倍率的纯函数核心（便于单测）。
func seedanceRatios(spec seedanceModelSpec, resolution string, duration int) map[string]float64 {
	ratios := map[string]float64{
		"duration": seedanceDurationRatio(duration),
	}
	if spec.Dynamic {
		if resolution == "" {
			resolution = seedanceDefaultResolution
		}
		ratios["resolution"] = seedanceResolutionRatio(spec.Base, resolution)
	}
	return ratios
}

// coolModelName 取面向 cool 上游的原始模型名（优先 UpstreamModelName）。
func coolModelName(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if info.UpstreamModelName != "" {
		return info.UpstreamModelName
	}
	return info.OriginModelName
}

func stripCoolPrefix(model string) string {
	return strings.TrimPrefix(strings.TrimSpace(model), "cool:")
}

func seedanceResolutionRatio(base, resolution string) float64 {
	ratios := seedanceStandardResolutionRatios
	if base == "seedance_2_fast" {
		ratios = seedanceFastResolutionRatios
	}
	if r, ok := ratios[resolution]; ok {
		return r
	}
	// 不支持的清晰度（例如 fast 请求 1080p）回落到默认 720p 计费。
	return ratios[seedanceDefaultResolution]
}

func seedanceDurationRatio(duration int) float64 {
	if duration <= 0 {
		duration = seedanceDefaultDuration
	}
	return float64(duration) / seedanceBillingUnitSeconds
}

// ---------------------------------------------------------------------------
// 清晰度 / 时长解析 —— 同时供「计费」与「转发给 cool」共用（见 adaptor.go），
// 保证「按什么清晰度计费」== 「实际请求 cool 的清晰度」，避免错扣。
// ---------------------------------------------------------------------------

// coolRawRequest 用于补读 TaskSubmitReq 结构体未覆盖的顶层字段。
// cool / mozia 客户端按文档在顶层传 resolution / aspect_ratio / duration，
// 但 TaskSubmitReq 没有 resolution 字段会被丢弃；这里直接读原始 body 兜底。
type coolRawRequest struct {
	Resolution  string         `json:"resolution,omitempty"`
	AspectRatio string         `json:"aspect_ratio,omitempty"`
	Size        string         `json:"size,omitempty"`
	Duration    *int           `json:"duration,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

func readCoolRawRequest(c *gin.Context) coolRawRequest {
	var out coolRawRequest
	if c == nil {
		return out
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return out
	}
	raw, err := storage.Bytes()
	if err != nil || len(raw) == 0 {
		return out
	}
	_ = common.Unmarshal(raw, &out)
	return out
}

// resolveSeedanceResolution 统一解析清晰度并归一化为 480p / 720p / 1080p。
// 优先级：metadata.resolution > 顶层 resolution > req.Metadata.resolution。
// 解析不到合法清晰度时返回 ""，由调用方决定是否回落默认。
func resolveSeedanceResolution(c *gin.Context, req *relaycommon.TaskSubmitReq) string {
	body := readCoolRawRequest(c)
	raw := firstNonEmpty(
		stringFromMap(body.Metadata, "resolution"),
		body.Resolution,
		stringFromMap(req.Metadata, "resolution"),
	)
	return normalizeSeedanceResolution(raw)
}

func resolveSeedanceDuration(c *gin.Context, req *relaycommon.TaskSubmitReq) int {
	if req.Duration > 0 {
		return req.Duration
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(req.Seconds)); err == nil && seconds > 0 {
		return seconds
	}
	body := readCoolRawRequest(c)
	if body.Duration != nil && *body.Duration > 0 {
		return *body.Duration
	}
	if d := intFromMap(body.Metadata, "duration"); d > 0 {
		return d
	}
	if d := intFromMap(req.Metadata, "duration"); d > 0 {
		return d
	}
	return seedanceDefaultDuration
}

func normalizeSeedanceResolution(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	v = strings.ReplaceAll(v, " ", "")
	v = strings.ReplaceAll(v, "_", "")
	v = strings.ReplaceAll(v, "-", "")
	switch {
	case v == "" || v == "adaptive":
		return ""
	case strings.Contains(v, "1080"):
		return "1080p"
	case strings.Contains(v, "720"):
		return "720p"
	case strings.Contains(v, "480"):
		return "480p"
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func stringFromMap(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok {
		return ""
	}
	switch v := value.(type) {
	case string:
		return v
	case float64:
		if v == float64(int(v)) {
			return strconv.Itoa(int(v))
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	default:
		return ""
	}
}

func intFromMap(values map[string]any, key string) int {
	if values == nil {
		return 0
	}
	switch v := values[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return 0
}
