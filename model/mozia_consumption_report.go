package model

import "errors"

// MoziaConsumptionReportRow contains net quota movements, not recharge receipts.
// Subscription allowance is separate: its purchase/gift provenance is not in
// the wallet ledger and must not be guessed from the subscription's presence.
type MoziaConsumptionReportRow struct {
	UserId      int    `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	ModelName   string `json:"model_name"`
	Source      string `json:"source"`
	Quota       int64  `json:"quota"`
}

func GetMoziaConsumptionReport(start, end int64) ([]MoziaConsumptionReportRow, error) {
	if start <= 0 || end <= start || end-start > 31*86400 {
		return nil, errors.New("statistics range must be positive and at most 31 days")
	}
	rows := make([]MoziaConsumptionReportRow, 0)
	// Keep refunds negative, including refunds of earlier days. Clamping each
	// user/source to zero would inflate the platform total and break reconciliation.
	if err := DB.Model(&MoziaWalletTransaction{}).
		Select("user_id, model_name, source, -SUM(delta) AS quota").
		Where("created_time >= ? AND created_time < ?", start, end).
		Where("event_type IN ?", []string{MoziaWalletEventConsume, MoziaWalletEventRefund}).
		Group("user_id, model_name, source").Scan(&rows).Error; err != nil {
		return nil, err
	}

	// Logs and wallets can live in different databases. Aggregate on the log
	// connection rather than joining them. Other is compact JSON written by the
	// gateway; a request-body copy escapes quotes and cannot match this marker.
	var subscriptions []MoziaConsumptionReportRow
	if err := LOG_DB.Model(&Log{}).
		Select("user_id, model_name, SUM(CASE WHEN type = 2 THEN quota ELSE -quota END) AS quota").
		Where("created_at >= ? AND created_at < ?", start, end).
		Where("type IN ?", []int{LogTypeConsume, LogTypeRefund}).
		Where("other LIKE ?", `%"billing_source":"subscription"%`).
		Group("user_id, model_name").Scan(&subscriptions).Error; err != nil {
		return nil, err
	}
	for _, row := range subscriptions {
		row.Source = "subscription"
		rows = append(rows, row)
	}

	ids := make([]int, 0)
	seen := make(map[int]bool)
	for _, row := range rows {
		if !seen[row.UserId] {
			seen[row.UserId] = true
			ids = append(ids, row.UserId)
		}
	}
	names := make(map[int]User)
	for offset := 0; offset < len(ids); offset += 500 {
		var users []User
		if err := DB.Unscoped().Select("id, username, display_name").
			Where("id IN ?", ids[offset:min(offset+500, len(ids))]).Find(&users).Error; err != nil {
			return nil, err
		}
		for _, user := range users {
			names[user.Id] = user
		}
	}
	for i := range rows {
		rows[i].Username = names[rows[i].UserId].Username
		rows[i].DisplayName = names[rows[i].UserId].DisplayName
	}
	return rows, nil
}
