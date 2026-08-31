package cool

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

const seedanceDefaultResolution = "720p"

// seedanceModelSpec 是 Seedance SKU 模型名解析结果。
type seedanceModelSpec struct {
	Base       string // 发给 cool 的真实 model key：seedance_2 / seedance_2_fast
	Resolution string // 480p / 720p / 1080p；动态 SKU 为空
	Dynamic    bool   // 无分辨率后缀，按请求参数动态路由（默认 720p）
	OK         bool   // 是否为受支持的 Seedance 2.0 SKU
}

// parseSeedanceModel 把对外 SKU 名（可带 cool: 前缀、分辨率后缀、_video 后缀）
// 解析回 cool 上游基础模型 + 分辨率。非 Seedance 2.0 模型返回 OK=false。
func parseSeedanceModel(model string) seedanceModelSpec {
	m := stripCoolPrefix(model)
	spec := seedanceModelSpec{}
	if strings.HasSuffix(m, "_video") {
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

func stripCoolPrefix(model string) string {
	return strings.TrimPrefix(strings.TrimSpace(model), "cool:")
}

// coolRawRequest 用于读取覆盖处理后的原始顶层字段，并兼容 metadata 参数。
type coolRawRequest struct {
	Resolution string         `json:"resolution,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
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
// 优先级：顶层 resolution > metadata.resolution。
// 解析不到合法清晰度时返回 ""，由调用方决定是否回落默认。
func resolveSeedanceResolution(c *gin.Context, req *relaycommon.TaskSubmitReq) string {
	body := readCoolRawRequest(c)
	raw := firstNonEmpty(
		body.Resolution,
		derefString(req.Resolution),
		stringFromMap(body.Metadata, "resolution"),
		stringFromMap(req.Metadata, "resolution"),
	)
	return normalizeSeedanceResolution(raw)
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
