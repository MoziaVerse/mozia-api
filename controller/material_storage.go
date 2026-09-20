package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

// 素材落我方对象存储（SeaweedFS，S3 兼容）的路径。原 Cool 代理路径保留在 material.go，
// 只有 MATERIAL_S3_* 配齐时才走这里。

const (
	materialMaxImageBytes = 20 << 20
	materialMaxAudioBytes = 50 << 20
	materialImportTimeout = 2 * time.Minute
)

// materialMimeKinds 是允许落盘的类型白名单。对象以判定出的类型写 Content-Type。
var materialMimeKinds = map[string]struct{ kind, ext string }{
	"image/png":       {"image", "png"},
	"image/jpeg":      {"image", "jpg"},
	"image/webp":      {"image", "webp"},
	"image/gif":       {"image", "gif"},
	"video/mp4":       {"video", "mp4"},
	"video/webm":      {"video", "webm"},
	"video/quicktime": {"video", "mov"},
	"video/x-msvideo": {"video", "avi"},
	"audio/mpeg":      {"audio", "mp3"},
	"audio/wav":       {"audio", "wav"},
	"audio/x-wav":     {"audio", "wav"},
	"audio/wave":      {"audio", "wav"},
	"audio/ogg":       {"audio", "ogg"},
	"audio/mp4":       {"audio", "m4a"},
	"audio/x-m4a":     {"audio", "m4a"},
	"audio/aac":       {"audio", "aac"},
	"audio/flac":      {"audio", "flac"},
	"application/ogg": {"audio", "ogg"}, // Go 的 sniff 对 ogg 给的是这个
}

// materialExtMimes 用于 sniff 认不出（application/octet-stream）时按扩展名回退。
var materialExtMimes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".gif": "image/gif",
	".mp4": "video/mp4", ".webm": "video/webm", ".mov": "video/quicktime", ".avi": "video/x-msvideo",
	".mp3": "audio/mpeg", ".wav": "audio/wav", ".ogg": "audio/ogg", ".m4a": "audio/mp4", ".aac": "audio/aac", ".flac": "audio/flac",
}

func materialMaxBytes(kind string) int64 {
	global := int64(constant.MaxRequestBodyMB) << 20
	if global <= 0 {
		global = 128 << 20 // 与 common/init.go 的默认值一致；测试进程里 constant 未初始化
	}
	switch kind {
	case "image":
		return minInt64(materialMaxImageBytes, global)
	case "audio":
		return minInt64(materialMaxAudioBytes, global)
	default:
		return global
	}
}

func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// uploadMaterialToStorage 处理 multipart 上传：取 file 字段 → 落临时文件 → 判型/限长 → PUT 对象存储。
func uploadMaterialToStorage(c *gin.Context, storage common.BodyStorage, boundary string, oss *service.MaterialObjectStorage) {
	reader, err := storage.NewReader()
	if err != nil {
		materialError(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}
	defer reader.Close()

	mr := multipart.NewReader(reader, boundary)
	var filePart *multipart.Part
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			materialError(c, http.StatusBadRequest, "invalid_request_error", "Invalid multipart body")
			return
		}
		if part.FormName() == "file" {
			filePart = part
			break
		}
		_ = part.Close()
	}
	if filePart == nil {
		materialError(c, http.StatusBadRequest, "invalid_request_error", "file field is required")
		return
	}
	defer filePart.Close()

	storeMaterial(c, oss, filePart, filePart.FileName(), filePart.Header.Get("Content-Type"), "upload")
}

// importMaterialToStorage 处理 upload_url：网关自己下载源地址再落盘。
// 源地址已经过 ValidateURLWithFetchSetting；这里额外禁止重定向——
// 校验过的公网 URL 一跳就能到内网。
func importMaterialToStorage(c *gin.Context, sourceURL string, filename string, oss *service.MaterialObjectStorage) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), materialImportTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		materialError(c, http.StatusBadRequest, "invalid_request_error", "url must be a public HTTP(S) URL")
		return
	}
	request.Header.Set("User-Agent", "MoziaMaterialImport/1.0")
	client, err := materialImportClient(ctx, request.URL.Hostname())
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Material import URL rejected at dial: %v", err))
		materialError(c, http.StatusBadRequest, "invalid_request_error", "url is not allowed")
		return
	}
	response, err := client.Do(request)
	if err != nil {
		materialError(c, http.StatusBadRequest, "invalid_request_error", "failed to fetch url")
		return
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		materialError(c, http.StatusBadRequest, "invalid_request_error", "url redirects are not allowed")
		return
	}
	if response.StatusCode != http.StatusOK {
		materialError(c, http.StatusBadRequest, "invalid_request_error", fmt.Sprintf("url returned HTTP %d", response.StatusCode))
		return
	}
	if filename == "" {
		filename = path.Base(request.URL.Path)
	}
	storeMaterial(c, oss, response.Body, filename, response.Header.Get("Content-Type"), "import")
}

// materialImportClient 先把源站域名解析成 IP 并按 SSRF 设置过滤，然后把连接钉在这个 IP 上，
// 避免"校验时解析到公网、连接时解析到内网"的 DNS 重绑定。
func materialImportClient(ctx context.Context, hostname string) (*http.Client, error) {
	fetchSetting := system_setting.GetFetchSetting()
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil || len(addrs) == 0 {
		return nil, fmt.Errorf("resolve %s: %w", hostname, err)
	}
	var pinned net.IP
	for _, addr := range addrs {
		ip := addr.IP
		// common.IsPrivateIP 只列了 v4 私网段；ip.IsPrivate 补上 fc00::/7
		if fetchSetting.EnableSSRFProtection && !fetchSetting.AllowPrivateIp && (common.IsPrivateIP(ip) || ip.IsPrivate()) {
			return nil, fmt.Errorf("%s resolves to private address %s", hostname, ip)
		}
		// 网关机大概率没有 v6 出网，优先钉 v4
		if pinned == nil || (pinned.To4() == nil && ip.To4() != nil) {
			pinned = ip
		}
	}
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(pinned.String(), port))
		},
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   materialImportTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

// storeMaterial 是两条入口共用的落盘段：临时文件 + sha256 + sniff + 白名单 + 体积上限 + PUT。
func storeMaterial(c *gin.Context, oss *service.MaterialObjectStorage, src io.Reader, filename string, declaredType string, source string) {
	tmp, err := os.CreateTemp("", "material-*")
	if err != nil {
		materialError(c, http.StatusInternalServerError, "server_error", "Failed to buffer material")
		return
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	// 先读 512 字节判型，再决定该类型的体积上限。
	head := make([]byte, 512)
	n, err := io.ReadFull(src, head)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		materialError(c, http.StatusBadRequest, "invalid_request_error", "Failed to read material")
		return
	}
	head = head[:n]
	if n == 0 {
		materialError(c, http.StatusBadRequest, "invalid_request_error", "material is empty")
		return
	}
	mimeType, ok := detectMaterialMime(head, declaredType, filename)
	if !ok {
		materialError(c, http.StatusUnsupportedMediaType, "unsupported_media_type", "material type is not supported; allowed: image, video, audio")
		return
	}
	meta := materialMimeKinds[mimeType]
	maxBytes := materialMaxBytes(meta.kind)

	hasher := sha256.New()
	limited := io.LimitReader(io.MultiReader(bytes.NewReader(head), src), maxBytes+1)
	size, err := io.Copy(io.MultiWriter(tmp, hasher), limited)
	if err != nil {
		materialError(c, http.StatusBadRequest, "invalid_request_error", "Failed to read material")
		return
	}
	if size > maxBytes {
		materialError(c, http.StatusRequestEntityTooLarge, "request_too_large", fmt.Sprintf("%s material must be at most %d MB", meta.kind, maxBytes>>20))
		return
	}
	sum := hex.EncodeToString(hasher.Sum(nil))
	if _, err = tmp.Seek(0, io.SeekStart); err != nil {
		materialError(c, http.StatusInternalServerError, "server_error", "Failed to buffer material")
		return
	}

	key := oss.ObjectKey(sum, meta.ext)
	if err = oss.PutObject(c.Request.Context(), key, mimeType, tmp, size, sum); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Material storage put failed: %v", err))
		materialError(c, http.StatusBadGateway, "upstream_error", "Failed to store material")
		return
	}
	fileURL, err := oss.PublicURL(key)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Material storage presign failed: %v", err))
		materialError(c, http.StatusInternalServerError, "server_error", "Failed to sign material url")
		return
	}

	record := &model.Material{
		UserId:    common.GetContextKeyInt(c, constant.ContextKeyUserId),
		TokenId:   common.GetContextKeyInt(c, constant.ContextKeyTokenId),
		Sha256:    sum,
		Size:      size,
		Mime:      mimeType,
		Kind:      meta.kind,
		ObjectKey: key,
		Url:       fileURL,
		FileName:  truncateString(filename, 255),
		Source:    source,
		CreatedAt: time.Now().Unix(),
	}
	if err = record.Insert(); err != nil {
		// 记录只服务风控，失败不影响上传结果
		logger.LogError(c.Request.Context(), fmt.Sprintf("Material record insert failed: %v", err))
	}

	c.JSON(http.StatusOK, gin.H{
		"file_url":  fileURL,
		"file_type": meta.kind,
		"file_name": filename,
		"sha256":    sum,
		"size":      size,
	})
}

// detectMaterialMime 先 sniff，认不出再看声明的 Content-Type，最后看扩展名；结果必须在白名单里。
func detectMaterialMime(head []byte, declaredType string, filename string) (string, bool) {
	candidates := []string{http.DetectContentType(head)}
	if declaredType != "" {
		if parsed, _, err := mime.ParseMediaType(declaredType); err == nil {
			candidates = append(candidates, parsed)
		}
	}
	if ext := strings.ToLower(path.Ext(filename)); ext != "" {
		if byExt, ok := materialExtMimes[ext]; ok {
			candidates = append(candidates, byExt)
		}
	}
	for _, candidate := range candidates {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == "application/octet-stream" || candidate == "" {
			continue
		}
		if _, ok := materialMimeKinds[candidate]; ok {
			return candidate, true
		}
		// sniff 给出了一个明确但不在白名单里的类型（text/plain、application/pdf…），直接拒
		if candidate == candidates[0] {
			return "", false
		}
	}
	return "", false
}

func truncateString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
