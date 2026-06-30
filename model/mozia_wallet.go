package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const (
	MoziaWalletSourceGift   = "gift"
	MoziaWalletSourcePaid   = "paid"
	MoziaWalletSourceLegacy = "legacy"

	MoziaWalletEventRegisterGift = "register_gift"
	MoziaWalletEventInviteGift   = "invite_gift"
	MoziaWalletEventTopUp        = "topup"
	MoziaWalletEventRedeem       = "redeem"
	MoziaWalletEventConsume      = "consume"
	MoziaWalletEventRefund       = "refund"
	MoziaWalletEventSettle       = "settle"
	MoziaWalletEventLegacySync   = "legacy_sync"
	MoziaWalletEventAdjust       = "adjust"

	MoziaWalletReservationReserved = "reserved"
	MoziaWalletReservationSettled  = "settled"
	MoziaWalletReservationRefunded = "refunded"

	MoziaQuotaPolicyMatchExact    = "exact"
	MoziaQuotaPolicyMatchPrefix   = "prefix"
	MoziaQuotaPolicyMatchWildcard = "wildcard"

	MoziaQuotaPolicyConsumeGiftFirst = "gift_first"
	MoziaQuotaPolicyConsumePaidFirst = "paid_first"
)

var (
	ErrMoziaWalletInsufficient     = errors.New("mozia wallet quota insufficient")
	ErrMoziaWalletSourceForbidden  = errors.New("mozia wallet source forbidden")
	ErrMoziaWalletReservationFinal = errors.New("mozia wallet reservation already refunded")
)

type MoziaWalletBalance struct {
	Id          int    `json:"id"`
	UserId      int    `json:"user_id" gorm:"not null;uniqueIndex:uk_mozia_wallet_user_source,priority:1"`
	Source      string `json:"source" gorm:"type:varchar(32);not null;uniqueIndex:uk_mozia_wallet_user_source,priority:2"`
	Balance     int    `json:"balance" gorm:"type:int;not null"`
	CreatedTime int64  `json:"created_time" gorm:"bigint"`
	UpdatedTime int64  `json:"updated_time" gorm:"bigint"`
}

// MoziaWalletTransaction is a sidecar ledger for Mozia quota source accounting.
// It intentionally does not replace logs; logs stay user-facing, this is audit data.
type MoziaWalletTransaction struct {
	Id            int    `json:"id"`
	UserId        int    `json:"user_id" gorm:"index;not null"`
	Source        string `json:"source" gorm:"type:varchar(32);index;not null"`
	Delta         int    `json:"delta" gorm:"type:int;not null"`
	BalanceAfter  int    `json:"balance_after" gorm:"type:int;not null"`
	EventType     string `json:"event_type" gorm:"type:varchar(64);index;not null"`
	ReferenceType string `json:"reference_type" gorm:"type:varchar(64);index"`
	ReferenceId   string `json:"reference_id" gorm:"type:varchar(128);index"`
	RequestId     string `json:"request_id" gorm:"type:varchar(64);index"`
	ModelName     string `json:"model_name" gorm:"type:varchar(255);index"`
	Metadata      string `json:"metadata" gorm:"type:text"`
	CreatedTime   int64  `json:"created_time" gorm:"bigint;index"`
}

type MoziaWalletReservation struct {
	Id                  int    `json:"id"`
	RequestId           string `json:"request_id" gorm:"type:varchar(64);uniqueIndex;not null"`
	UserId              int    `json:"user_id" gorm:"index;not null"`
	ModelName           string `json:"model_name" gorm:"type:varchar(255);index"`
	Status              string `json:"status" gorm:"type:varchar(32);index;not null"`
	PreConsumedQuota    int    `json:"pre_consumed_quota" gorm:"type:int;not null"`
	ActualQuota         int    `json:"actual_quota" gorm:"type:int;not null"`
	GiftQuota           int    `json:"gift_quota" gorm:"type:int;not null"`
	PaidQuota           int    `json:"paid_quota" gorm:"type:int;not null"`
	LegacyQuota         int    `json:"legacy_quota" gorm:"type:int;not null"`
	ConsumeOrder        string `json:"consume_order" gorm:"type:varchar(32);not null"`
	PolicyAllowedSource string `json:"policy_allowed_source" gorm:"type:varchar(128)"`
	CreatedTime         int64  `json:"created_time" gorm:"bigint"`
	UpdatedTime         int64  `json:"updated_time" gorm:"bigint"`
}

type MoziaModelQuotaPolicy struct {
	Id             int    `json:"id"`
	ModelPattern   string `json:"model_pattern" gorm:"type:varchar(255);not null;index:idx_mozia_quota_policy_match"`
	MatchType      string `json:"match_type" gorm:"type:varchar(32);not null;index:idx_mozia_quota_policy_match"`
	AllowedSources string `json:"allowed_sources" gorm:"type:varchar(128);not null"`
	ConsumeOrder   string `json:"consume_order" gorm:"type:varchar(32);not null"`
	Enabled        bool   `json:"enabled" gorm:"index"`
	Priority       int    `json:"priority" gorm:"type:int;index"`
	CreatedTime    int64  `json:"created_time" gorm:"bigint"`
	UpdatedTime    int64  `json:"updated_time" gorm:"bigint"`
}

type MoziaWalletGrantInput struct {
	UserId        int
	Source        string
	Amount        int
	EventType     string
	ReferenceType string
	ReferenceId   string
	RequestId     string
	ModelName     string
	Metadata      map[string]interface{}
}

type MoziaWalletView struct {
	UserId  int            `json:"user_id"`
	Total   int            `json:"total"`
	Sources map[string]int `json:"sources"`
}

type moziaQuotaPolicyDecision struct {
	AllowedSources []string
	ConsumeOrder   string
	Policy         *MoziaModelQuotaPolicy
}

func normalizeMoziaWalletSource(source string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case MoziaWalletSourceGift:
		return MoziaWalletSourceGift, nil
	case MoziaWalletSourcePaid:
		return MoziaWalletSourcePaid, nil
	case MoziaWalletSourceLegacy:
		return MoziaWalletSourceLegacy, nil
	default:
		return "", fmt.Errorf("invalid mozia wallet source: %s", source)
	}
}

func normalizeMoziaConsumeOrder(order string) string {
	switch strings.ToLower(strings.TrimSpace(order)) {
	case MoziaQuotaPolicyConsumePaidFirst:
		return MoziaQuotaPolicyConsumePaidFirst
	default:
		return MoziaQuotaPolicyConsumeGiftFirst
	}
}

func normalizeMoziaMatchType(matchType string) string {
	switch strings.ToLower(strings.TrimSpace(matchType)) {
	case MoziaQuotaPolicyMatchPrefix:
		return MoziaQuotaPolicyMatchPrefix
	case MoziaQuotaPolicyMatchWildcard:
		return MoziaQuotaPolicyMatchWildcard
	default:
		return MoziaQuotaPolicyMatchExact
	}
}

func parseMoziaSources(value string) []string {
	seen := map[string]bool{}
	var result []string
	for _, part := range strings.Split(value, ",") {
		source, err := normalizeMoziaWalletSource(part)
		if err != nil || seen[source] {
			continue
		}
		seen[source] = true
		result = append(result, source)
	}
	return result
}

func formatMoziaSources(sources []string) string {
	cleaned := make([]string, 0, len(sources))
	seen := map[string]bool{}
	for _, source := range sources {
		normalized, err := normalizeMoziaWalletSource(source)
		if err != nil || seen[normalized] {
			continue
		}
		seen[normalized] = true
		cleaned = append(cleaned, normalized)
	}
	return strings.Join(cleaned, ",")
}

func moziaDefaultPolicyDecision() moziaQuotaPolicyDecision {
	return moziaQuotaPolicyDecision{
		AllowedSources: []string{MoziaWalletSourceGift, MoziaWalletSourcePaid, MoziaWalletSourceLegacy},
		ConsumeOrder:   MoziaQuotaPolicyConsumeGiftFirst,
	}
}

func moziaMetadataString(metadata map[string]interface{}) (string, error) {
	if len(metadata) == 0 {
		return "", nil
	}
	b, err := common.Marshal(metadata)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func updateMoziaUserQuotaCache(userId int, delta int) {
	if delta == 0 {
		return
	}
	gopool.Go(func() {
		var err error
		if delta > 0 {
			err = cacheIncrUserQuota(userId, int64(delta))
		} else {
			err = cacheDecrUserQuota(userId, int64(-delta))
		}
		if err != nil {
			common.SysLog("failed to update mozia wallet user quota cache: " + err.Error())
		}
	})
}

func ensureMoziaWalletBalanceTx(tx *gorm.DB, userId int, source string) error {
	now := common.GetTimestamp()
	balance := MoziaWalletBalance{
		UserId:      userId,
		Source:      source,
		Balance:     0,
		CreatedTime: now,
		UpdatedTime: now,
	}
	return tx.FirstOrCreate(&balance, MoziaWalletBalance{UserId: userId, Source: source}).Error
}

func getMoziaWalletBalanceTx(tx *gorm.DB, userId int, source string) (int, error) {
	var balance int
	err := tx.Model(&MoziaWalletBalance{}).
		Where("user_id = ? AND source = ?", userId, source).
		Select("balance").
		Scan(&balance).Error
	return balance, err
}

func createMoziaWalletTransactionTx(tx *gorm.DB, input MoziaWalletGrantInput, delta int, balanceAfter int) error {
	metadata, err := moziaMetadataString(input.Metadata)
	if err != nil {
		return err
	}
	eventType := strings.TrimSpace(input.EventType)
	if eventType == "" {
		eventType = MoziaWalletEventAdjust
	}
	return tx.Create(&MoziaWalletTransaction{
		UserId:        input.UserId,
		Source:        input.Source,
		Delta:         delta,
		BalanceAfter:  balanceAfter,
		EventType:     eventType,
		ReferenceType: strings.TrimSpace(input.ReferenceType),
		ReferenceId:   strings.TrimSpace(input.ReferenceId),
		RequestId:     strings.TrimSpace(input.RequestId),
		ModelName:     strings.TrimSpace(input.ModelName),
		Metadata:      metadata,
		CreatedTime:   common.GetTimestamp(),
	}).Error
}

func grantMoziaWalletQuotaTx(tx *gorm.DB, input MoziaWalletGrantInput, mirrorUserQuota bool) (int, error) {
	if input.UserId == 0 {
		return 0, errors.New("user id is empty")
	}
	if input.Amount <= 0 {
		return 0, errors.New("quota must be positive")
	}
	source, err := normalizeMoziaWalletSource(input.Source)
	if err != nil {
		return 0, err
	}
	input.Source = source
	if err := ensureMoziaWalletBalanceTx(tx, input.UserId, source); err != nil {
		return 0, err
	}
	now := common.GetTimestamp()
	if err := tx.Model(&MoziaWalletBalance{}).
		Where("user_id = ? AND source = ?", input.UserId, source).
		Updates(map[string]interface{}{
			"balance":      gorm.Expr("balance + ?", input.Amount),
			"updated_time": now,
		}).Error; err != nil {
		return 0, err
	}
	balanceAfter, err := getMoziaWalletBalanceTx(tx, input.UserId, source)
	if err != nil {
		return 0, err
	}
	if err := createMoziaWalletTransactionTx(tx, input, input.Amount, balanceAfter); err != nil {
		return 0, err
	}
	if mirrorUserQuota {
		if err := tx.Model(&User{}).
			Where("id = ?", input.UserId).
			Update("quota", gorm.Expr("quota + ?", input.Amount)).Error; err != nil {
			return 0, err
		}
	}
	return balanceAfter, nil
}

func GrantMoziaWalletQuota(input MoziaWalletGrantInput) error {
	err := DB.Transaction(func(tx *gorm.DB) error {
		_, err := grantMoziaWalletQuotaTx(tx, input, true)
		return err
	})
	if err == nil {
		updateMoziaUserQuotaCache(input.UserId, input.Amount)
	}
	return err
}

func RecordMoziaInitialGiftQuota(userId int, amount int, referenceType string, referenceId string) error {
	if amount <= 0 {
		return nil
	}
	input := MoziaWalletGrantInput{
		UserId:        userId,
		Source:        MoziaWalletSourceGift,
		Amount:        amount,
		EventType:     MoziaWalletEventRegisterGift,
		ReferenceType: referenceType,
		ReferenceId:   referenceId,
	}
	return DB.Transaction(func(tx *gorm.DB) error {
		_, err := grantMoziaWalletQuotaTx(tx, input, false)
		return err
	})
}

func syncMoziaLegacyBalanceForUserTx(tx *gorm.DB, userId int) error {
	if userId == 0 {
		return errors.New("user id is empty")
	}
	var userQuota int
	if err := tx.Model(&User{}).Where("id = ?", userId).Select("quota").Scan(&userQuota).Error; err != nil {
		return err
	}
	var total int
	if err := tx.Model(&MoziaWalletBalance{}).Where("user_id = ?", userId).Select("COALESCE(SUM(balance), 0)").Scan(&total).Error; err != nil {
		return err
	}
	if userQuota <= total {
		return nil
	}
	delta := userQuota - total
	_, err := grantMoziaWalletQuotaTx(tx, MoziaWalletGrantInput{
		UserId:        userId,
		Source:        MoziaWalletSourceLegacy,
		Amount:        delta,
		EventType:     MoziaWalletEventLegacySync,
		ReferenceType: "users.quota",
		ReferenceId:   fmt.Sprintf("%d", userId),
	}, false)
	return err
}

func GetMoziaWalletView(userId int) (*MoziaWalletView, error) {
	if err := DB.Transaction(func(tx *gorm.DB) error {
		return syncMoziaLegacyBalanceForUserTx(tx, userId)
	}); err != nil {
		return nil, err
	}
	var balances []MoziaWalletBalance
	if err := DB.Where("user_id = ?", userId).Find(&balances).Error; err != nil {
		return nil, err
	}
	view := &MoziaWalletView{
		UserId:  userId,
		Sources: map[string]int{},
	}
	for _, balance := range balances {
		view.Sources[balance.Source] = balance.Balance
		view.Total += balance.Balance
	}
	for _, source := range []string{MoziaWalletSourceGift, MoziaWalletSourcePaid, MoziaWalletSourceLegacy} {
		if _, ok := view.Sources[source]; !ok {
			view.Sources[source] = 0
		}
	}
	return view, nil
}

func CreateMoziaModelQuotaPolicy(policy *MoziaModelQuotaPolicy) error {
	if policy == nil {
		return errors.New("policy is nil")
	}
	policy.ModelPattern = strings.TrimSpace(policy.ModelPattern)
	if policy.ModelPattern == "" {
		return errors.New("model_pattern is empty")
	}
	policy.MatchType = normalizeMoziaMatchType(policy.MatchType)
	policy.AllowedSources = formatMoziaSources(parseMoziaSources(policy.AllowedSources))
	if policy.AllowedSources == "" {
		return errors.New("allowed_sources is empty")
	}
	policy.ConsumeOrder = normalizeMoziaConsumeOrder(policy.ConsumeOrder)
	now := common.GetTimestamp()
	policy.CreatedTime = now
	policy.UpdatedTime = now
	return DB.Create(policy).Error
}

func UpdateMoziaModelQuotaPolicy(policy *MoziaModelQuotaPolicy) error {
	if policy == nil || policy.Id == 0 {
		return errors.New("policy id is empty")
	}
	policy.ModelPattern = strings.TrimSpace(policy.ModelPattern)
	if policy.ModelPattern == "" {
		return errors.New("model_pattern is empty")
	}
	policy.MatchType = normalizeMoziaMatchType(policy.MatchType)
	policy.AllowedSources = formatMoziaSources(parseMoziaSources(policy.AllowedSources))
	if policy.AllowedSources == "" {
		return errors.New("allowed_sources is empty")
	}
	policy.ConsumeOrder = normalizeMoziaConsumeOrder(policy.ConsumeOrder)
	policy.UpdatedTime = common.GetTimestamp()
	return DB.Model(&MoziaModelQuotaPolicy{}).Where("id = ?", policy.Id).Updates(map[string]interface{}{
		"model_pattern":   policy.ModelPattern,
		"match_type":      policy.MatchType,
		"allowed_sources": policy.AllowedSources,
		"consume_order":   policy.ConsumeOrder,
		"enabled":         policy.Enabled,
		"priority":        policy.Priority,
		"updated_time":    policy.UpdatedTime,
	}).Error
}

func DeleteMoziaModelQuotaPolicy(id int) error {
	if id == 0 {
		return errors.New("policy id is empty")
	}
	return DB.Delete(&MoziaModelQuotaPolicy{}, id).Error
}

func GetMoziaModelQuotaPolicyByID(id int) (*MoziaModelQuotaPolicy, error) {
	var policy MoziaModelQuotaPolicy
	if err := DB.First(&policy, id).Error; err != nil {
		return nil, err
	}
	return &policy, nil
}

func GetAllMoziaModelQuotaPolicies() ([]MoziaModelQuotaPolicy, error) {
	var policies []MoziaModelQuotaPolicy
	err := DB.Order("priority DESC, id DESC").Find(&policies).Error
	return policies, err
}

func modelMatchesMoziaPolicy(policy MoziaModelQuotaPolicy, modelName string) bool {
	pattern := strings.TrimSpace(policy.ModelPattern)
	switch normalizeMoziaMatchType(policy.MatchType) {
	case MoziaQuotaPolicyMatchPrefix:
		return strings.HasPrefix(modelName, pattern)
	case MoziaQuotaPolicyMatchWildcard:
		return simpleMoziaWildcardMatch(pattern, modelName)
	default:
		return modelName == pattern
	}
}

func simpleMoziaWildcardMatch(pattern string, value string) bool {
	if pattern == "*" {
		return true
	}
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return pattern == value
	}
	if parts[0] != "" && !strings.HasPrefix(value, parts[0]) {
		return false
	}
	if last := parts[len(parts)-1]; last != "" && !strings.HasSuffix(value, last) {
		return false
	}
	pos := 0
	for _, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(value[pos:], part)
		if idx < 0 {
			return false
		}
		pos += idx + len(part)
	}
	return true
}

func getMoziaQuotaPolicyDecision(modelName string) (moziaQuotaPolicyDecision, error) {
	return getMoziaQuotaPolicyDecisionTx(DB, modelName)
}

func getMoziaQuotaPolicyDecisionTx(tx *gorm.DB, modelName string) (moziaQuotaPolicyDecision, error) {
	var policies []MoziaModelQuotaPolicy
	if err := tx.Where("enabled = ?", true).Find(&policies).Error; err != nil {
		return moziaDefaultPolicyDecision(), err
	}
	if len(policies) == 0 {
		return moziaDefaultPolicyDecision(), nil
	}
	sort.SliceStable(policies, func(i, j int) bool {
		if policies[i].Priority != policies[j].Priority {
			return policies[i].Priority > policies[j].Priority
		}
		if policies[i].MatchType != policies[j].MatchType {
			rank := map[string]int{
				MoziaQuotaPolicyMatchExact:    3,
				MoziaQuotaPolicyMatchPrefix:   2,
				MoziaQuotaPolicyMatchWildcard: 1,
			}
			return rank[normalizeMoziaMatchType(policies[i].MatchType)] > rank[normalizeMoziaMatchType(policies[j].MatchType)]
		}
		return len(policies[i].ModelPattern) > len(policies[j].ModelPattern)
	})
	for i := range policies {
		if !modelMatchesMoziaPolicy(policies[i], modelName) {
			continue
		}
		allowed := parseMoziaSources(policies[i].AllowedSources)
		if len(allowed) == 0 {
			return moziaDefaultPolicyDecision(), nil
		}
		return moziaQuotaPolicyDecision{
			AllowedSources: allowed,
			ConsumeOrder:   normalizeMoziaConsumeOrder(policies[i].ConsumeOrder),
			Policy:         &policies[i],
		}, nil
	}
	return moziaDefaultPolicyDecision(), nil
}

func sourceAllowedByMoziaPolicy(source string, allowed []string) bool {
	for _, item := range allowed {
		if item == source {
			return true
		}
	}
	return false
}

func orderedMoziaSources(decision moziaQuotaPolicyDecision) []string {
	var base []string
	if decision.ConsumeOrder == MoziaQuotaPolicyConsumePaidFirst {
		base = []string{MoziaWalletSourcePaid, MoziaWalletSourceGift, MoziaWalletSourceLegacy}
	} else {
		base = []string{MoziaWalletSourceGift, MoziaWalletSourcePaid, MoziaWalletSourceLegacy}
	}
	out := make([]string, 0, len(base))
	for _, source := range base {
		if sourceAllowedByMoziaPolicy(source, decision.AllowedSources) {
			out = append(out, source)
		}
	}
	return out
}

func CheckMoziaQuotaPolicyAccess(userId int, modelName string) error {
	decision, err := getMoziaQuotaPolicyDecision(modelName)
	if err != nil {
		return err
	}
	if decision.Policy == nil {
		return nil
	}
	if err := DB.Transaction(func(tx *gorm.DB) error {
		return syncMoziaLegacyBalanceForUserTx(tx, userId)
	}); err != nil {
		return err
	}
	balances, err := GetMoziaWalletView(userId)
	if err != nil {
		return err
	}
	for _, source := range decision.AllowedSources {
		if balances.Sources[source] > 0 {
			return nil
		}
	}
	hasSub, subErr := HasActiveUserSubscription(userId)
	if subErr == nil && hasSub && sourceAllowedByMoziaPolicy(MoziaWalletSourcePaid, decision.AllowedSources) {
		return nil
	}
	return fmt.Errorf("%w: model %s requires quota source %s", ErrMoziaWalletSourceForbidden, modelName, strings.Join(decision.AllowedSources, ","))
}

func FilterPricingByMoziaWalletAccess(userId int, pricing []Pricing) []Pricing {
	if userId == 0 || len(pricing) == 0 {
		return pricing
	}
	filtered := make([]Pricing, 0, len(pricing))
	for _, item := range pricing {
		if err := CheckMoziaQuotaPolicyAccess(userId, item.ModelName); err == nil {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func ReserveMoziaWalletQuota(requestId string, userId int, modelName string, amount int) error {
	if amount <= 0 {
		return nil
	}
	if strings.TrimSpace(requestId) == "" {
		requestId = common.NewRequestId()
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		return settleMoziaWalletReservationTx(tx, requestId, userId, modelName, amount, false)
	})
	if err == nil {
		updateMoziaUserQuotaCache(userId, -amount)
	}
	return err
}

func SettleMoziaWalletReservation(requestId string, userId int, modelName string, actualQuota int) error {
	if actualQuota < 0 {
		return errors.New("actual quota cannot be negative")
	}
	var cacheDelta int
	err := DB.Transaction(func(tx *gorm.DB) error {
		before, after, err := settleMoziaWalletReservationWithDeltaTx(tx, requestId, userId, modelName, actualQuota, true)
		cacheDelta = after - before
		return err
	})
	if err == nil && cacheDelta != 0 {
		updateMoziaUserQuotaCache(userId, -cacheDelta)
	}
	return err
}

func RefundMoziaWalletReservation(requestId string, userId int) error {
	if strings.TrimSpace(requestId) == "" {
		return nil
	}
	var refundTotal int
	err := DB.Transaction(func(tx *gorm.DB) error {
		var reservation MoziaWalletReservation
		if err := tx.Where("request_id = ? AND user_id = ?", requestId, userId).First(&reservation).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if reservation.Status == MoziaWalletReservationRefunded {
			return nil
		}
		refundTotal = reservation.GiftQuota + reservation.PaidQuota + reservation.LegacyQuota
		if refundTotal <= 0 {
			reservation.Status = MoziaWalletReservationRefunded
			reservation.UpdatedTime = common.GetTimestamp()
			return tx.Save(&reservation).Error
		}
		if err := refundMoziaWalletSourcesTx(tx, &reservation, refundTotal); err != nil {
			return err
		}
		reservation.Status = MoziaWalletReservationRefunded
		reservation.ActualQuota = 0
		reservation.PreConsumedQuota = 0
		reservation.GiftQuota = 0
		reservation.PaidQuota = 0
		reservation.LegacyQuota = 0
		reservation.UpdatedTime = common.GetTimestamp()
		return tx.Save(&reservation).Error
	})
	if err == nil && refundTotal > 0 {
		updateMoziaUserQuotaCache(userId, refundTotal)
	}
	return err
}

func settleMoziaWalletReservationWithDeltaTx(tx *gorm.DB, requestId string, userId int, modelName string, actualQuota int, allowExisting bool) (int, int, error) {
	if strings.TrimSpace(requestId) == "" {
		return 0, 0, errors.New("request id is empty")
	}
	var existing MoziaWalletReservation
	err := tx.Where("request_id = ? AND user_id = ?", requestId, userId).First(&existing).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if actualQuota <= 0 {
				return 0, 0, nil
			}
			if err := settleMoziaWalletReservationTx(tx, requestId, userId, modelName, actualQuota, true); err != nil {
				return 0, 0, err
			}
			return 0, actualQuota, nil
		}
		return 0, 0, err
	}
	if !allowExisting && existing.Status != "" {
		return existing.GiftQuota + existing.PaidQuota + existing.LegacyQuota, existing.GiftQuota + existing.PaidQuota + existing.LegacyQuota, nil
	}
	if existing.Status == MoziaWalletReservationRefunded {
		return 0, 0, ErrMoziaWalletReservationFinal
	}
	before := existing.GiftQuota + existing.PaidQuota + existing.LegacyQuota
	delta := actualQuota - before
	if delta > 0 {
		if err := allocateMoziaWalletSourcesTx(tx, &existing, delta); err != nil {
			return before, before, err
		}
	} else if delta < 0 {
		if err := refundMoziaWalletSourcesTx(tx, &existing, -delta); err != nil {
			return before, before, err
		}
	}
	existing.ActualQuota = actualQuota
	existing.PreConsumedQuota = actualQuota
	existing.Status = MoziaWalletReservationSettled
	existing.UpdatedTime = common.GetTimestamp()
	if err := tx.Save(&existing).Error; err != nil {
		return before, before, err
	}
	return before, actualQuota, nil
}

func settleMoziaWalletReservationTx(tx *gorm.DB, requestId string, userId int, modelName string, amount int, settled bool) error {
	if amount <= 0 {
		return nil
	}
	if err := syncMoziaLegacyBalanceForUserTx(tx, userId); err != nil {
		return err
	}
	decision, err := getMoziaQuotaPolicyDecisionTx(tx, modelName)
	if err != nil {
		return err
	}
	if len(orderedMoziaSources(decision)) == 0 {
		return fmt.Errorf("%w: model %s has no allowed quota sources", ErrMoziaWalletSourceForbidden, modelName)
	}
	now := common.GetTimestamp()
	reservation := MoziaWalletReservation{
		RequestId:           requestId,
		UserId:              userId,
		ModelName:           modelName,
		Status:              MoziaWalletReservationReserved,
		PreConsumedQuota:    amount,
		ActualQuota:         amount,
		ConsumeOrder:        decision.ConsumeOrder,
		PolicyAllowedSource: strings.Join(decision.AllowedSources, ","),
		CreatedTime:         now,
		UpdatedTime:         now,
	}
	if settled {
		reservation.Status = MoziaWalletReservationSettled
	}
	if err := allocateMoziaWalletSourcesTx(tx, &reservation, amount); err != nil {
		return err
	}
	if err := tx.Create(&reservation).Error; err != nil {
		return err
	}
	return nil
}

func allocateMoziaWalletSourcesTx(tx *gorm.DB, reservation *MoziaWalletReservation, amount int) error {
	if amount <= 0 {
		return nil
	}
	decision := moziaQuotaPolicyDecision{
		AllowedSources: parseMoziaSources(reservation.PolicyAllowedSource),
		ConsumeOrder:   normalizeMoziaConsumeOrder(reservation.ConsumeOrder),
	}
	if len(decision.AllowedSources) == 0 {
		decision, _ = getMoziaQuotaPolicyDecisionTx(tx, reservation.ModelName)
	}
	remaining := amount
	for _, source := range orderedMoziaSources(decision) {
		if remaining <= 0 {
			break
		}
		if err := ensureMoziaWalletBalanceTx(tx, reservation.UserId, source); err != nil {
			return err
		}
		balance, err := getMoziaWalletBalanceTx(tx, reservation.UserId, source)
		if err != nil {
			return err
		}
		use := balance
		if use > remaining {
			use = remaining
		}
		if use <= 0 {
			continue
		}
		res := tx.Model(&MoziaWalletBalance{}).
			Where("user_id = ? AND source = ? AND balance >= ?", reservation.UserId, source, use).
			Updates(map[string]interface{}{
				"balance":      gorm.Expr("balance - ?", use),
				"updated_time": common.GetTimestamp(),
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: concurrent balance update", ErrMoziaWalletInsufficient)
		}
		balanceAfter, err := getMoziaWalletBalanceTx(tx, reservation.UserId, source)
		if err != nil {
			return err
		}
		if err := createMoziaWalletTransactionTx(tx, MoziaWalletGrantInput{
			UserId:        reservation.UserId,
			Source:        source,
			EventType:     MoziaWalletEventConsume,
			ReferenceType: "reservation",
			ReferenceId:   reservation.RequestId,
			RequestId:     reservation.RequestId,
			ModelName:     reservation.ModelName,
		}, -use, balanceAfter); err != nil {
			return err
		}
		addMoziaReservedSource(reservation, source, use)
		remaining -= use
	}
	if remaining > 0 {
		return fmt.Errorf("%w: need %d more quota for model %s", ErrMoziaWalletInsufficient, remaining, reservation.ModelName)
	}
	res := tx.Model(&User{}).
		Where("id = ? AND quota >= ?", reservation.UserId, amount).
		Update("quota", gorm.Expr("quota - ?", amount))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("%w: user quota mirror is insufficient", ErrMoziaWalletInsufficient)
	}
	return nil
}

func refundMoziaWalletSourcesTx(tx *gorm.DB, reservation *MoziaWalletReservation, amount int) error {
	if amount <= 0 {
		return nil
	}
	remaining := amount
	for _, source := range reverseMoziaReservationSources(reservation) {
		if remaining <= 0 {
			break
		}
		available := getMoziaReservedSource(reservation, source)
		use := available
		if use > remaining {
			use = remaining
		}
		if use <= 0 {
			continue
		}
		if err := ensureMoziaWalletBalanceTx(tx, reservation.UserId, source); err != nil {
			return err
		}
		if err := tx.Model(&MoziaWalletBalance{}).
			Where("user_id = ? AND source = ?", reservation.UserId, source).
			Updates(map[string]interface{}{
				"balance":      gorm.Expr("balance + ?", use),
				"updated_time": common.GetTimestamp(),
			}).Error; err != nil {
			return err
		}
		balanceAfter, err := getMoziaWalletBalanceTx(tx, reservation.UserId, source)
		if err != nil {
			return err
		}
		if err := createMoziaWalletTransactionTx(tx, MoziaWalletGrantInput{
			UserId:        reservation.UserId,
			Source:        source,
			EventType:     MoziaWalletEventRefund,
			ReferenceType: "reservation",
			ReferenceId:   reservation.RequestId,
			RequestId:     reservation.RequestId,
			ModelName:     reservation.ModelName,
		}, use, balanceAfter); err != nil {
			return err
		}
		addMoziaReservedSource(reservation, source, -use)
		remaining -= use
	}
	if remaining > 0 {
		return fmt.Errorf("reservation source quota is inconsistent: missing %d", remaining)
	}
	return tx.Model(&User{}).
		Where("id = ?", reservation.UserId).
		Update("quota", gorm.Expr("quota + ?", amount)).Error
}

func addMoziaReservedSource(reservation *MoziaWalletReservation, source string, delta int) {
	switch source {
	case MoziaWalletSourceGift:
		reservation.GiftQuota += delta
	case MoziaWalletSourcePaid:
		reservation.PaidQuota += delta
	case MoziaWalletSourceLegacy:
		reservation.LegacyQuota += delta
	}
}

func getMoziaReservedSource(reservation *MoziaWalletReservation, source string) int {
	switch source {
	case MoziaWalletSourceGift:
		return reservation.GiftQuota
	case MoziaWalletSourcePaid:
		return reservation.PaidQuota
	case MoziaWalletSourceLegacy:
		return reservation.LegacyQuota
	default:
		return 0
	}
}

func reverseMoziaReservationSources(reservation *MoziaWalletReservation) []string {
	if normalizeMoziaConsumeOrder(reservation.ConsumeOrder) == MoziaQuotaPolicyConsumePaidFirst {
		return []string{MoziaWalletSourceLegacy, MoziaWalletSourceGift, MoziaWalletSourcePaid}
	}
	return []string{MoziaWalletSourceLegacy, MoziaWalletSourcePaid, MoziaWalletSourceGift}
}
