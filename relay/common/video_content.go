package common

import (
	"fmt"
	"strings"
)

const (
	taskContentTypeText     = "text"
	taskContentTypeImageURL = "image_url"
	taskContentTypeVideoURL = "video_url"
	taskContentTypeAudioURL = "audio_url"

	taskContentRoleFirstFrame     = "first_frame"
	taskContentRoleLastFrame      = "last_frame"
	taskContentRoleReferenceImage = "reference_image"
	taskContentRoleReferenceVideo = "reference_video"
	taskContentRoleReferenceAudio = "reference_audio"
)

type TaskContentURL struct {
	URL string `json:"url,omitempty"`
}

type TaskContentItem struct {
	Type     string          `json:"type"`
	Role     string          `json:"role,omitempty"`
	Text     string          `json:"text,omitempty"`
	ImageURL *TaskContentURL `json:"image_url,omitempty"`
	VideoURL *TaskContentURL `json:"video_url,omitempty"`
	AudioURL *TaskContentURL `json:"audio_url,omitempty"`
}

type VideoContentSummary struct {
	Prompt          string
	FirstFrameURL   string
	LastFrameURL    string
	ReferenceImages []string
	ReferenceVideos []string
	ReferenceAudios []string
}

func (s VideoContentSummary) LegacyImages() []string {
	if s.FirstFrameURL == "" && s.LastFrameURL == "" && len(s.ReferenceImages) == 0 {
		return nil
	}

	images := make([]string, 0, len(s.ReferenceImages)+2)
	if s.FirstFrameURL != "" {
		images = append(images, s.FirstFrameURL)
	}
	if s.LastFrameURL != "" {
		images = append(images, s.LastFrameURL)
	}
	images = append(images, s.ReferenceImages...)
	return images
}

func (t *TaskSubmitReq) ParseVideoContent() (VideoContentSummary, error) {
	var summary VideoContentSummary
	if t == nil {
		return summary, nil
	}

	topLevelPrompt := strings.TrimSpace(t.Prompt)
	summary.Prompt = topLevelPrompt
	for i, item := range t.Content {
		itemType := strings.TrimSpace(item.Type)
		role := strings.TrimSpace(item.Role)

		switch itemType {
		case taskContentTypeText:
			if role != "" {
				return VideoContentSummary{}, fmt.Errorf("content[%d] role %q is invalid for text", i, role)
			}
			text := strings.TrimSpace(item.Text)
			if topLevelPrompt == "" && text != "" {
				if summary.Prompt != "" {
					return VideoContentSummary{}, fmt.Errorf("content[%d] has a second text prompt", i)
				}
				summary.Prompt = text
			}
		case taskContentTypeImageURL:
			urlValue, err := contentURLValue(i, taskContentTypeImageURL, item.ImageURL)
			if err != nil {
				return VideoContentSummary{}, err
			}
			switch role {
			case taskContentRoleFirstFrame:
				if summary.FirstFrameURL != "" {
					return VideoContentSummary{}, fmt.Errorf("content[%d] duplicate first_frame", i)
				}
				summary.FirstFrameURL = urlValue
			case taskContentRoleLastFrame:
				if summary.LastFrameURL != "" {
					return VideoContentSummary{}, fmt.Errorf("content[%d] duplicate last_frame", i)
				}
				summary.LastFrameURL = urlValue
			case taskContentRoleReferenceImage:
				summary.ReferenceImages = append(summary.ReferenceImages, urlValue)
			default:
				return VideoContentSummary{}, fmt.Errorf("content[%d] role %q is invalid for image_url", i, role)
			}
		case taskContentTypeVideoURL:
			if role != taskContentRoleReferenceVideo {
				return VideoContentSummary{}, fmt.Errorf("content[%d] role %q is invalid for video_url", i, role)
			}
			urlValue, err := contentURLValue(i, taskContentTypeVideoURL, item.VideoURL)
			if err != nil {
				return VideoContentSummary{}, err
			}
			summary.ReferenceVideos = append(summary.ReferenceVideos, urlValue)
		case taskContentTypeAudioURL:
			if role != taskContentRoleReferenceAudio {
				return VideoContentSummary{}, fmt.Errorf("content[%d] role %q is invalid for audio_url", i, role)
			}
			urlValue, err := contentURLValue(i, taskContentTypeAudioURL, item.AudioURL)
			if err != nil {
				return VideoContentSummary{}, err
			}
			summary.ReferenceAudios = append(summary.ReferenceAudios, urlValue)
		default:
			return VideoContentSummary{}, fmt.Errorf("content[%d] type %q is invalid", i, itemType)
		}
	}

	if summary.LastFrameURL != "" && summary.FirstFrameURL == "" {
		return VideoContentSummary{}, fmt.Errorf("last_frame requires first_frame")
	}
	if (summary.FirstFrameURL != "" || summary.LastFrameURL != "") &&
		(len(summary.ReferenceImages) > 0 || len(summary.ReferenceVideos) > 0 || len(summary.ReferenceAudios) > 0) {
		return VideoContentSummary{}, fmt.Errorf("frame images cannot be mixed with reference materials")
	}

	return summary, nil
}

func contentURLValue(index int, contentType string, value *TaskContentURL) (string, error) {
	if value == nil {
		return "", fmt.Errorf("content[%d] %s.url is required", index, contentType)
	}
	urlValue := strings.TrimSpace(value.URL)
	if urlValue == "" {
		return "", fmt.Errorf("content[%d] %s.url is required", index, contentType)
	}
	return urlValue, nil
}
