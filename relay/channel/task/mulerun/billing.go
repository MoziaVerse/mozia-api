package mulerun

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

const (
	seedanceDefaultDurationSeconds = 15
	seedanceBillingUnitSeconds     = 5
)

var (
	seedanceStandardResolutionRatios = map[string]float64{
		"480p":  1,
		"720p":  0.71 / 0.33,
		"1080p": 1.82 / 0.33,
	}
	seedanceFastResolutionRatios = map[string]float64{
		"480p": 1,
		"720p": 0.58 / 0.25,
	}
)

type seedanceBillingRequest struct {
	Resolution string         `json:"resolution,omitempty"`
	Size       string         `json:"size,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type seedanceBillingParams struct {
	Resolution string
	Duration   int
}

// EstimateBilling applies Mulerun Seedance parameter multipliers while leaving
// other Mulerun Studio endpoints on the default fixed-price path.
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	ep := LookupStudioByShortKey(info.Action)
	if ep == nil || !isSeedanceModel(ep.CLIID) {
		return nil
	}

	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}

	return seedanceBillingRatios(ep.CLIID, resolveSeedanceBillingParams(c, req))
}

func isSeedanceModel(modelID string) bool {
	return strings.HasPrefix(modelID, "bytedance/seedance-2.0/") ||
		strings.HasPrefix(modelID, "bytedance/seedance-2.0-fast/")
}

func isSeedanceFastModel(modelID string) bool {
	return strings.HasPrefix(modelID, "bytedance/seedance-2.0-fast/")
}

func resolveSeedanceBillingParams(c *gin.Context, req relaycommon.TaskSubmitReq) seedanceBillingParams {
	body := readSeedanceBillingRequest(c)
	resolution := firstNonEmpty(
		body.Resolution,
		stringFromMap(body.Metadata, "resolution"),
		stringFromMap(req.Metadata, "resolution"),
		req.Size,
		body.Size,
	)

	return seedanceBillingParams{
		Resolution: normalizeSeedanceResolution(resolution),
		Duration:   resolveSeedanceDuration(req),
	}
}

func readSeedanceBillingRequest(c *gin.Context) seedanceBillingRequest {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return seedanceBillingRequest{}
	}
	raw, err := storage.Bytes()
	if err != nil || len(raw) == 0 {
		return seedanceBillingRequest{}
	}

	var req seedanceBillingRequest
	if err := common.Unmarshal(raw, &req); err != nil {
		return seedanceBillingRequest{}
	}
	return req
}

func resolveSeedanceDuration(req relaycommon.TaskSubmitReq) int {
	if req.Duration > 0 {
		return req.Duration
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(req.Seconds)); err == nil && seconds > 0 {
		return seconds
	}
	return seedanceDefaultDurationSeconds
}

func normalizeSeedanceResolution(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	v = strings.ReplaceAll(v, " ", "")
	v = strings.ReplaceAll(v, "_", "")
	v = strings.ReplaceAll(v, "-", "")
	if v == "" || v == "adaptive" {
		return ""
	}
	if strings.Contains(v, "1080") {
		return "1080p"
	}
	if strings.Contains(v, "720") {
		return "720p"
	}
	if strings.Contains(v, "480") {
		return "480p"
	}
	return ""
}

func seedanceBillingRatios(modelID string, params seedanceBillingParams) map[string]float64 {
	resolution := params.Resolution
	if resolution == "" {
		resolution = defaultSeedanceResolution(modelID)
	}

	return map[string]float64{
		"duration":   seedanceDurationRatio(params.Duration),
		"resolution": seedanceResolutionRatio(modelID, resolution),
	}
}

func defaultSeedanceResolution(modelID string) string {
	if isSeedanceFastModel(modelID) {
		return "720p"
	}
	return "1080p"
}

func seedanceDurationRatio(duration int) float64 {
	if duration <= 0 {
		duration = seedanceDefaultDurationSeconds
	}
	return float64(duration) / seedanceBillingUnitSeconds
}

func seedanceResolutionRatio(modelID, resolution string) float64 {
	ratios := seedanceStandardResolutionRatios
	if isSeedanceFastModel(modelID) {
		ratios = seedanceFastResolutionRatios
	}
	if ratio, ok := ratios[resolution]; ok {
		return ratio
	}
	return ratios[defaultSeedanceResolution(modelID)]
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
