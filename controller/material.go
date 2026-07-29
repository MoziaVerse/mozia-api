package controller

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

const maxMaterialUpstreamResponseBytes = 1 << 20

type materialImportRequest struct {
	URL      string `json:"url"`
	Filename string `json:"filename,omitempty"`
}

func UploadMaterial(c *gin.Context) {
	contentType, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || contentType != "multipart/form-data" {
		materialError(c, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be multipart/form-data")
		return
	}
	if strings.TrimSpace(params["boundary"]) == "" {
		materialError(c, http.StatusBadRequest, "invalid_request_error", "multipart boundary is required")
		return
	}

	proxyMaterialRequest(c, "/v1/cool/upload")
}

func ImportMaterial(c *gin.Context) {
	contentType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || contentType != "application/json" {
		materialError(c, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		materialBodyError(c, err)
		return
	}
	defer common.CleanupBodyStorage(c)
	var request materialImportRequest
	if err = common.UnmarshalBodyReusable(c, &request); err != nil {
		materialError(c, http.StatusBadRequest, "invalid_request_error", "Invalid JSON request body")
		return
	}

	request.URL = strings.TrimSpace(request.URL)
	parsedURL, err := url.Parse(request.URL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Hostname() == "" || parsedURL.User != nil {
		materialError(c, http.StatusBadRequest, "invalid_request_error", "url must be a public HTTP(S) URL")
		return
	}
	fetchSetting := system_setting.GetFetchSetting()
	if err = common.ValidateURLWithFetchSetting(request.URL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Material import URL rejected: %v", err))
		materialError(c, http.StatusBadRequest, "invalid_request_error", "url is not allowed")
		return
	}

	proxyMaterialStorage(c, storage, "/v1/cool/upload_url")
}

func proxyMaterialRequest(c *gin.Context, upstreamPath string) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		materialBodyError(c, err)
		return
	}
	defer common.CleanupBodyStorage(c)
	proxyMaterialStorage(c, storage, upstreamPath)
}

func proxyMaterialStorage(c *gin.Context, storage common.BodyStorage, upstreamPath string) {
	if storage.Size() == 0 {
		materialError(c, http.StatusBadRequest, "invalid_request_error", "Request body is required")
		return
	}
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		materialError(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
		return
	}

	upstream, err := service.ResolveMaterialUpstream(
		common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
		common.GetContextKeyString(c, constant.ContextKeyUserGroup),
	)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve material channel: %v", err))
		materialError(c, http.StatusServiceUnavailable, "material_channel_unavailable", "Material service is unavailable")
		return
	}

	request, err := http.NewRequestWithContext(
		c.Request.Context(),
		http.MethodPost,
		upstream.BaseURL+upstreamPath,
		common.ReaderOnly(storage),
	)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create material upstream request: %v", err))
		materialError(c, http.StatusBadGateway, "upstream_error", "Failed to contact material service")
		return
	}
	request.ContentLength = storage.Size()
	request.Header.Set("Authorization", "Bearer "+upstream.APIKey)
	request.Header.Set("Content-Type", c.GetHeader("Content-Type"))
	request.Header.Set("Accept", "application/json")

	client, err := service.GetHttpClientWithProxy(upstream.Proxy)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create material upstream client for channel %d: %v", upstream.ChannelID, err))
		materialError(c, http.StatusBadGateway, "upstream_error", "Failed to contact material service")
		return
	}
	if client == nil {
		client = http.DefaultClient
	}

	response, err := client.Do(request)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Material upstream request failed for channel %d: %v", upstream.ChannelID, err))
		materialError(c, http.StatusBadGateway, "upstream_error", "Failed to contact material service")
		return
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(response.Body, maxMaterialUpstreamResponseBytes+1))
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to read material upstream response for channel %d: %v", upstream.ChannelID, err))
		materialError(c, http.StatusBadGateway, "upstream_error", "Invalid response from material service")
		return
	}
	if len(responseBody) > maxMaterialUpstreamResponseBytes {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Material upstream response exceeded limit for channel %d", upstream.ChannelID))
		materialError(c, http.StatusBadGateway, "upstream_error", "Invalid response from material service")
		return
	}

	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		c.Header("Content-Type", contentType)
	}
	c.Status(response.StatusCode)
	_, _ = c.Writer.Write(responseBody)
}

func materialBodyError(c *gin.Context, err error) {
	if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
		materialError(c, http.StatusRequestEntityTooLarge, "request_too_large", "Request body is too large")
		return
	}
	materialError(c, http.StatusBadRequest, "invalid_request_error", "Failed to read request body")
}

func materialError(c *gin.Context, status int, errorType string, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    errorType,
		},
	})
}
