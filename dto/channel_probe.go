package dto

type ChannelDiscoveryDTO struct {
	ChannelID        int    `json:"channel_id"`
	ChannelName      string `json:"channel_name"`
	ChannelType      int    `json:"channel_type"`
	ProviderIdentity string `json:"provider_identity"`
	BaseURLIdentity  string `json:"base_url_identity"`
	Enabled          bool   `json:"enabled"`
	ProbeCapability  bool   `json:"probe_capability"`
}

type ChannelBalanceProbeDTO struct {
	Status                string   `json:"status"`
	Supported             bool     `json:"supported"`
	Balance               *float64 `json:"balance"`
	UnitOrCurrency        string   `json:"unit_or_currency"`
	CheckedAt             int64    `json:"checked_at"`
	ProviderIdentity      string   `json:"provider_identity"`
	SanitizedErrorCode    string   `json:"sanitized_error_code,omitempty"`
	SanitizedErrorMessage string   `json:"sanitized_error_message,omitempty"`
}
