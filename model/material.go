package model

import "github.com/QuantumNous/new-api/common"

// GetEnabledChannelsByTypeAndGroup returns complete channel records, including
// credentials, for internal service-side channel selection.
func GetEnabledChannelsByTypeAndGroup(channelType int, group string) ([]*Channel, error) {
	var channels []*Channel
	query := DB.Where("type = ? AND status = ?", channelType, common.ChannelStatusEnabled)
	group = NormalizeChannelGroupFilter(group)
	if group != "" {
		groupColumn := commonGroupCol
		if groupColumn == "" {
			if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
				groupColumn = `"group"`
			} else {
				groupColumn = "`group`"
			}
		}
		if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
			query = query.Where("CONCAT(',', "+groupColumn+", ',') LIKE ? ESCAPE '!'", channelGroupFilterPattern(group))
		} else {
			query = query.Where("(',' || "+groupColumn+" || ',') LIKE ? ESCAPE '!'", channelGroupFilterPattern(group))
		}
	}
	err := query.Order("priority DESC").Order("id ASC").Find(&channels).Error
	return channels, err
}
