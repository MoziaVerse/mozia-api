package controller

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// videoProxyError returns a standardized OpenAI-style error response.
func videoProxyError(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    errType,
		},
	})
}

func VideoProxy(c *gin.Context) {
	serveVideoProxy(c, "")
}

func SignedVideoProxy(c *gin.Context) {
	userID, filename, ok := verifySignedVideoProxyRequest(c)
	if !ok {
		return
	}
	c.Set("id", userID)
	serveVideoProxy(c, filename)
}

func verifySignedVideoProxyRequest(c *gin.Context) (int, string, bool) {
	taskID := c.Param("task_id")
	if taskID == "" {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "task_id is required")
		return 0, "", false
	}
	filename := taskcommon.SanitizeVideoFilename(c.Param("filename"), taskID)
	userID, err := strconv.Atoi(strings.TrimSpace(c.Query("uid")))
	if err != nil || userID <= 0 {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "uid is required")
		return 0, "", false
	}
	expires, err := strconv.ParseInt(strings.TrimSpace(c.Query("expires")), 10, 64)
	if err != nil || expires <= 0 {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "expires is required")
		return 0, "", false
	}
	now := time.Now()
	if now.Unix() > expires {
		videoProxyError(c, http.StatusForbidden, "permission_error", "Signature expired")
		return 0, "", false
	}
	signature := strings.TrimSpace(c.Query("signature"))
	if signature == "" {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "signature is required")
		return 0, "", false
	}
	if err := taskcommon.VerifySignedVideoProxy(userID, taskID, filename, expires, signature, now); err != nil {
		if errors.Is(err, taskcommon.ErrSignedVideoExpired) {
			videoProxyError(c, http.StatusForbidden, "permission_error", "Signature expired")
		} else {
			videoProxyError(c, http.StatusForbidden, "permission_error", "Invalid signature")
		}
		return 0, "", false
	}
	return userID, filename, true
}

func serveVideoProxy(c *gin.Context, attachmentFilename string) {
	taskID := c.Param("task_id")
	userID := c.GetInt("id")
	task, exists, err := model.GetByTaskId(userID, taskID)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to query task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to query task")
		return
	}
	if !exists || task == nil {
		videoProxyError(c, http.StatusNotFound, "invalid_request_error", "Task not found")
		return
	}

	if task.Status != model.TaskStatusSuccess {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("Task is not completed yet, current status: %s", task.Status))
		return
	}

	channel, err := model.CacheGetChannel(task.ChannelId)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to get channel for task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to retrieve channel information")
		return
	}
	baseURL := strings.TrimSpace(channel.GetBaseURL())

	var videoURL string
	proxy := channel.GetSetting().Proxy
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create proxy client for task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy client")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "", nil)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create request: %s", err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy request")
		return
	}

	switch channel.Type {
	case constant.ChannelTypeGemini:
		apiKey := task.PrivateData.Key
		if apiKey == "" {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Missing stored API key for Gemini task %s", taskID))
			videoProxyError(c, http.StatusInternalServerError, "server_error", "API key not stored for task")
			return
		}
		videoURL, err = getGeminiVideoURL(channel, task, apiKey)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve Gemini video URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to resolve Gemini video URL")
			return
		}
		req.Header.Set("x-goog-api-key", apiKey)
	case constant.ChannelTypeVertexAi:
		videoURL, err = getVertexVideoURL(channel, task)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve Vertex video URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to resolve Vertex video URL")
			return
		}
	case constant.ChannelTypeOpenAI, constant.ChannelTypeSora:
		if baseURL == "" {
			baseURL = "https://api.openai.com"
		}
		videoURL = fmt.Sprintf("%s/v1/videos/%s/content", baseURL, task.GetUpstreamTaskID())
		req.Header.Set("Authorization", "Bearer "+channel.Key)
	case constant.ChannelTypeMoziaSeedanceGen, constant.ChannelTypeMoziaSeedanceVideos:
		shouldAuthorize := false
		videoURL, shouldAuthorize = resolveMoziaVideoContentURL(channel.Type, baseURL, task)
		if shouldAuthorize {
			req.Header.Set("Authorization", "Bearer "+channel.Key)
		}
	default:
		// Video URL is stored in PrivateData.ResultURL (fallback to FailReason for old data)
		videoURL = task.GetResultURL()
	}

	videoURL = strings.TrimSpace(videoURL)
	if videoURL == "" {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Video URL is empty for task %s", taskID))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		return
	}

	if strings.HasPrefix(videoURL, "data:") {
		if err := writeVideoDataURL(c, videoURL, attachmentFilename); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to decode video data URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		}
		return
	}

	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(videoURL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Video URL blocked for task %s: %v", taskID, err))
		videoProxyError(c, http.StatusForbidden, "server_error", fmt.Sprintf("request blocked: %v", err))
		return
	}

	req.URL, err = url.Parse(videoURL)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to parse URL %s: %s", videoURL, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy request")
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to fetch video from %s: %s", videoURL, err.Error()))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Upstream returned status %d for %s", resp.StatusCode, videoURL))
		videoProxyError(c, http.StatusBadGateway, "server_error",
			fmt.Sprintf("Upstream service returned status %d", resp.StatusCode))
		return
	}

	for key, values := range resp.Header {
		if attachmentFilename != "" {
			switch http.CanonicalHeaderKey(key) {
			case "Accept-Ranges", "Content-Encoding", "Content-Length", "Content-Range", "Content-Type", "Etag", "Last-Modified":
			default:
				continue
			}
		}
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}

	if attachmentFilename != "" {
		c.Writer.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
			"filename": attachmentFilename,
		}))
		c.Writer.Header().Set("Cache-Control", fmt.Sprintf("private, max-age=%d", int(taskcommon.SignedVideoURLTTL/time.Second)))
	} else {
		c.Writer.Header().Set("Cache-Control", "public, max-age=86400")
	}
	c.Writer.WriteHeader(resp.StatusCode)
	if _, err = io.Copy(c.Writer, resp.Body); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to stream video content: %s", err.Error()))
	}
}

func resolveMoziaVideoContentURL(channelType int, baseURL string, task *model.Task) (string, bool) {
	if resultURL := strings.TrimSpace(task.GetResultURL()); resultURL != "" && !isTaskProxyContentURL(resultURL, task.TaskID) {
		return resultURL, false
	}

	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", false
	}
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}
	resourcePath := "/video/generations/"
	if channelType == constant.ChannelTypeMoziaSeedanceVideos {
		resourcePath = "/videos/"
	}
	return baseURL + resourcePath + url.PathEscape(task.GetUpstreamTaskID()) + "/content", true
}

func writeVideoDataURL(c *gin.Context, dataURL, attachmentFilename string) error {
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid data url")
	}

	header := parts[0]
	payload := parts[1]
	if !strings.HasPrefix(header, "data:") || !strings.Contains(header, ";base64") {
		return fmt.Errorf("unsupported data url")
	}

	mimeType := strings.TrimPrefix(header, "data:")
	mimeType = strings.TrimSuffix(mimeType, ";base64")
	if mimeType == "" {
		mimeType = "video/mp4"
	}

	videoBytes, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		videoBytes, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return err
		}
	}

	c.Writer.Header().Set("Content-Type", mimeType)
	if attachmentFilename != "" {
		c.Writer.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{
			"filename": attachmentFilename,
		}))
		c.Writer.Header().Set("Cache-Control", fmt.Sprintf("private, max-age=%d", int(taskcommon.SignedVideoURLTTL/time.Second)))
	} else {
		c.Writer.Header().Set("Cache-Control", "public, max-age=86400")
	}
	c.Writer.WriteHeader(http.StatusOK)
	_, err = c.Writer.Write(videoBytes)
	return err
}
