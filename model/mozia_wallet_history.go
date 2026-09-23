package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
)

// MoziaWalletHistoryItem is the customer-safe projection of the wallet ledger.
// ReferenceId, request details and internal metadata must never be returned here.
type MoziaWalletHistoryItem struct {
	Id           int    `json:"id"`
	Source       string `json:"source"`
	Delta        int    `json:"delta"`
	BalanceAfter int    `json:"balance_after"`
	EventType    string `json:"event_type"`
	PublicNote   string `json:"public_note"`
	CreatedTime  int64  `json:"created_time"`
}

type MoziaWalletHistory struct {
	Items      []MoziaWalletHistoryItem `json:"items"`
	NextCursor int                      `json:"next_cursor"`
}

func GetMoziaWalletHistory(userId, beforeId, limit int) (*MoziaWalletHistory, error) {
	if userId <= 0 || beforeId < 0 || limit < 1 || limit > 50 {
		return nil, errors.New("invalid wallet history query")
	}
	// Reservation refunds release pre-consumed quota; they are part of API usage,
	// not new credits. Legacy synchronization is also not a new receipt.
	query := DB.Model(&MoziaWalletTransaction{}).
		Where("user_id = ? AND delta <> 0", userId).
		Where("(event_type IN ? OR (event_type = ? AND (reference_type IS NULL OR reference_type <> ?)))",
			[]string{MoziaWalletEventRegisterGift, MoziaWalletEventInviteGift,
				MoziaWalletEventTopUp, MoziaWalletEventRedeem, MoziaWalletEventAdjust},
			MoziaWalletEventRefund, "reservation")
	if beforeId > 0 {
		query = query.Where("id < ?", beforeId)
	}
	var rows []MoziaWalletTransaction
	if err := query.Select("id", "source", "delta", "balance_after", "event_type", "metadata", "created_time").
		Order("id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := &MoziaWalletHistory{Items: make([]MoziaWalletHistoryItem, 0, len(rows))}
	if len(rows) > limit {
		rows = rows[:limit]
		result.NextCursor = rows[len(rows)-1].Id
	}
	for _, row := range rows {
		var metadata struct {
			PublicNote string `json:"public_note"`
		}
		if err := common.UnmarshalJsonStr(row.Metadata, &metadata); err != nil {
			metadata.PublicNote = ""
		}
		result.Items = append(result.Items, MoziaWalletHistoryItem{
			Id: row.Id, Source: row.Source, Delta: row.Delta,
			BalanceAfter: row.BalanceAfter, EventType: row.EventType,
			PublicNote: metadata.PublicNote, CreatedTime: row.CreatedTime,
		})
	}
	return result, nil
}
