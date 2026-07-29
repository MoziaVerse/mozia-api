package service

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

var ErrMaterialChannelUnavailable = errors.New("material channel unavailable")

type MaterialUpstream struct {
	ChannelID int
	BaseURL   string
	APIKey    string
	Proxy     string
}

func ResolveMaterialUpstream(usingGroup string, userGroup string) (*MaterialUpstream, error) {
	groups := []string{strings.TrimSpace(usingGroup)}
	if groups[0] == "" {
		groups[0] = strings.TrimSpace(userGroup)
	}
	if groups[0] == "auto" {
		groups = GetUserAutoGroup(userGroup)
	}

	seenGroups := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		group = model.NormalizeChannelGroupFilter(group)
		if group == "" {
			continue
		}
		if _, seen := seenGroups[group]; seen {
			continue
		}
		seenGroups[group] = struct{}{}

		channels, err := model.GetEnabledChannelsByTypeAndGroup(constant.ChannelTypeMoziaCool, group)
		if err != nil {
			return nil, err
		}
		for _, channel := range channels {
			apiKey, _, keyErr := channel.GetNextEnabledKey()
			if keyErr != nil || strings.TrimSpace(apiKey) == "" {
				continue
			}

			baseURL := strings.TrimSpace(channel.GetBaseURL())
			if baseURL == "" {
				baseURL = constant.ChannelBaseURLs[constant.ChannelTypeMoziaCool]
			}
			if baseURL == "" {
				continue
			}

			return &MaterialUpstream{
				ChannelID: channel.Id,
				BaseURL:   strings.TrimRight(baseURL, "/"),
				APIKey:    apiKey,
				Proxy:     channel.GetSetting().Proxy,
			}, nil
		}
	}

	return nil, ErrMaterialChannelUnavailable
}
