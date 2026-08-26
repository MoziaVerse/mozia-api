package model

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
)

type ResellerUsageItem struct {
	CustomerId       int    `json:"customer_id"`
	Model            string `json:"model"`
	RequestCount     int64  `json:"request_count"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	TotalTokens      int64  `json:"total_tokens"`
	CustomerQuota    int64  `json:"customer_quota"`
}

type ResellerUsageSummary struct {
	RequestCount     int64 `json:"request_count"`
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	CustomerQuota    int64 `json:"customer_quota"`
	ModelCount       int   `json:"model_count"`
}

type ResellerUsageResult struct {
	Summary       ResellerUsageSummary         `json:"summary"`
	Items         []ResellerUsageItem          `json:"items"`
	DailySpend    []ResellerDailyUsageSpend    `json:"daily_spend"`
	SubagentSpend []ResellerSubagentUsageSpend `json:"subagent_spend"`
}

type ResellerDailyUsageSpend struct {
	Date          string `json:"date"`
	CustomerQuota int64  `json:"customer_quota"`
}

type ResellerSubagentUsageSpend struct {
	SubagentMemberId int   `json:"subagent_member_id"`
	RequestCount     int64 `json:"request_count"`
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	CustomerQuota    int64 `json:"customer_quota"`
}

type ResellerTaskItem struct {
	CustomerId  int    `json:"customer_id"`
	TaskId      string `json:"task_id"`
	Platform    string `json:"platform"`
	Model       string `json:"model"`
	Action      string `json:"action"`
	Status      string `json:"status"`
	Progress    string `json:"progress"`
	SubmittedAt int64  `json:"submitted_at"`
	StartedAt   int64  `json:"started_at"`
	FinishedAt  int64  `json:"finished_at"`
}

type ResellerTaskPage struct {
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
	Total    int64              `json:"total"`
	Items    []ResellerTaskItem `json:"items"`
}

type resellerUsageSettlementRow struct {
	CustomerId          int
	SubagentMemberId    int
	ModelName           string
	ActualCustomerQuota int64
	UsageJSON           string
	CreatedAt           int64
}

type resellerTaskUsageJSON struct {
	Kind             string `json:"kind"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	TotalTokens      int64  `json:"total_tokens"`
}

type resellerTaskRow struct {
	ID         int64
	TaskID     string
	UserID     int
	Platform   constant.TaskPlatform
	Action     string
	Status     TaskStatus
	Progress   string
	SubmitTime int64
	StartTime  int64
	FinishTime int64
	Properties Properties
}

func ListResellerUsage(resellerId int, customerId *int, startTimestamp *int64, endTimestamp *int64, modelName *string, subagentMemberId *int) (*ResellerUsageResult, error) {
	// ponytail: parse usage JSON in Go for SQLite/MySQL parity; add normalized token columns if full-history scans become costly.
	rows := make([]resellerUsageSettlementRow, 0)
	query := DB.Model(&ResellerRequestSettlement{}).
		Select("reseller_request_settlements.customer_id, CASE WHEN usage_customer.subagent_member_id IS NOT NULL AND reseller_request_settlements.created_at >= usage_customer.subagent_assigned_at THEN usage_customer.subagent_member_id ELSE 0 END AS subagent_member_id, reseller_request_settlements.model_name, reseller_request_settlements.actual_customer_quota, reseller_request_settlements.usage_json, reseller_request_settlements.created_at").
		Joins("LEFT JOIN reseller_customers AS usage_customer ON usage_customer.id = reseller_request_settlements.customer_id AND usage_customer.reseller_id = reseller_request_settlements.reseller_id").
		Where("reseller_request_settlements.reseller_id = ? AND reseller_request_settlements.status = ?", resellerId, ResellerSettlementStatusSettled)
	if subagentMemberId != nil {
		query = query.Where("usage_customer.subagent_member_id = ? AND reseller_request_settlements.created_at >= usage_customer.subagent_assigned_at", *subagentMemberId)
	}
	if customerId != nil {
		query = query.Where("reseller_request_settlements.customer_id = ?", *customerId)
	}
	if startTimestamp != nil && endTimestamp != nil {
		query = query.Where("reseller_request_settlements.created_at >= ? AND reseller_request_settlements.created_at <= ?", *startTimestamp, *endTimestamp)
	}
	if modelName != nil {
		query = query.Where("reseller_request_settlements.model_name = ?", *modelName)
	}
	if err := query.Order("reseller_request_settlements.customer_id ASC").Order("reseller_request_settlements.model_name ASC").Order("reseller_request_settlements.id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}

	result := &ResellerUsageResult{
		Items:         make([]ResellerUsageItem, 0),
		DailySpend:    make([]ResellerDailyUsageSpend, 0),
		SubagentSpend: make([]ResellerSubagentUsageSpend, 0),
	}
	if len(rows) == 0 {
		return result, nil
	}

	aggregated := make(map[string]*ResellerUsageItem, len(rows))
	dailySpend := make(map[string]*ResellerDailyUsageSpend)
	subagentSpend := make(map[int]*ResellerSubagentUsageSpend)
	for _, row := range rows {
		modelName := strings.TrimSpace(row.ModelName)
		key := strconv.Itoa(row.CustomerId) + "\x00" + modelName
		item, ok := aggregated[key]
		if !ok {
			item = &ResellerUsageItem{
				CustomerId: row.CustomerId,
				Model:      modelName,
			}
			aggregated[key] = item
		}
		promptTokens, completionTokens, totalTokens := parseResellerUsageTokens(row.UsageJSON)
		date := time.Unix(row.CreatedAt, 0).UTC().Format("2006-01-02")
		day := dailySpend[date]
		if day == nil {
			day = &ResellerDailyUsageSpend{Date: date}
			dailySpend[date] = day
		}
		day.CustomerQuota += row.ActualCustomerQuota
		if row.SubagentMemberId > 0 {
			spend := subagentSpend[row.SubagentMemberId]
			if spend == nil {
				spend = &ResellerSubagentUsageSpend{SubagentMemberId: row.SubagentMemberId}
				subagentSpend[row.SubagentMemberId] = spend
			}
			spend.RequestCount++
			spend.PromptTokens += promptTokens
			spend.CompletionTokens += completionTokens
			spend.TotalTokens += totalTokens
			spend.CustomerQuota += row.ActualCustomerQuota
		}
		item.RequestCount++
		item.PromptTokens += promptTokens
		item.CompletionTokens += completionTokens
		item.TotalTokens += totalTokens
		item.CustomerQuota += row.ActualCustomerQuota
	}

	result.Items = make([]ResellerUsageItem, 0, len(aggregated))
	models := make(map[string]struct{}, len(aggregated))
	for _, item := range aggregated {
		result.Summary.RequestCount += item.RequestCount
		result.Summary.PromptTokens += item.PromptTokens
		result.Summary.CompletionTokens += item.CompletionTokens
		result.Summary.TotalTokens += item.TotalTokens
		result.Summary.CustomerQuota += item.CustomerQuota
		result.Items = append(result.Items, *item)
		models[item.Model] = struct{}{}
	}
	sort.Slice(result.Items, func(i, j int) bool {
		if result.Items[i].CustomerId != result.Items[j].CustomerId {
			return result.Items[i].CustomerId < result.Items[j].CustomerId
		}
		return result.Items[i].Model < result.Items[j].Model
	})
	result.Summary.ModelCount = len(models)
	for _, day := range dailySpend {
		result.DailySpend = append(result.DailySpend, *day)
	}
	sort.Slice(result.DailySpend, func(i, j int) bool {
		return result.DailySpend[i].Date < result.DailySpend[j].Date
	})
	for _, spend := range subagentSpend {
		result.SubagentSpend = append(result.SubagentSpend, *spend)
	}
	sort.Slice(result.SubagentSpend, func(i, j int) bool {
		if result.SubagentSpend[i].CustomerQuota != result.SubagentSpend[j].CustomerQuota {
			return result.SubagentSpend[i].CustomerQuota > result.SubagentSpend[j].CustomerQuota
		}
		return result.SubagentSpend[i].SubagentMemberId < result.SubagentSpend[j].SubagentMemberId
	})
	return result, nil
}

func ListResellerCustomerTasks(customer *ResellerCustomerRecord, page int, pageSize int, startTimestamp *int64, endTimestamp *int64, taskID *string) (*ResellerTaskPage, error) {
	taskPage := &ResellerTaskPage{
		Page:     page,
		PageSize: pageSize,
		Items:    make([]ResellerTaskItem, 0),
	}
	if customer == nil || customer.UserId <= 0 {
		return taskPage, nil
	}

	startBound := customer.JoinedAt
	if startTimestamp != nil && *startTimestamp > startBound {
		startBound = *startTimestamp
	}

	query := DB.Model(&Task{}).
		Where("user_id = ? AND submit_time >= ?", customer.UserId, startBound)
	if endTimestamp != nil {
		query = query.Where("submit_time <= ?", *endTimestamp)
	}
	if taskID != nil {
		query = query.Where("task_id = ?", *taskID)
	}

	if err := query.Count(&taskPage.Total).Error; err != nil {
		return nil, err
	}

	rows := make([]resellerTaskRow, 0, pageSize)
	offset := (page - 1) * pageSize
	if err := query.Select("id, user_id, task_id, platform, action, status, progress, submit_time, start_time, finish_time, properties").
		Order("submit_time DESC").
		Order("id DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	taskPage.Items = make([]ResellerTaskItem, 0, len(rows))
	for _, row := range rows {
		taskPage.Items = append(taskPage.Items, ResellerTaskItem{
			CustomerId:  customer.Id,
			TaskId:      row.TaskID,
			Platform:    string(row.Platform),
			Model:       strings.TrimSpace(row.Properties.OriginModelName),
			Action:      row.Action,
			Status:      string(row.Status),
			Progress:    row.Progress,
			SubmittedAt: row.SubmitTime,
			StartedAt:   row.StartTime,
			FinishedAt:  row.FinishTime,
		})
	}
	return taskPage, nil
}

func ListResellerTasks(resellerId int, page int, pageSize int, startTimestamp *int64, endTimestamp *int64, taskID *string, subagentMemberId *int) (*ResellerTaskPage, error) {
	taskPage := &ResellerTaskPage{
		Page:     page,
		PageSize: pageSize,
		Items:    make([]ResellerTaskItem, 0),
	}

	var customers []ResellerCustomerRecord
	var err error
	if subagentMemberId == nil {
		customers, err = ListResellerCustomerRecords(resellerId, false)
	} else {
		customers, err = ListResellerSubagentCustomerRecords(resellerId, *subagentMemberId)
		for i := range customers {
			if customers[i].SubagentAssignedAt > customers[i].JoinedAt {
				customers[i].JoinedAt = customers[i].SubagentAssignedAt
			}
		}
	}
	if err != nil {
		return nil, err
	}

	userCustomerMap := make(map[int]ResellerCustomerRecord, len(customers))
	for _, customer := range customers {
		if customer.UserId <= 0 {
			continue
		}
		current, ok := userCustomerMap[customer.UserId]
		if !ok || customer.JoinedAt > current.JoinedAt || (customer.JoinedAt == current.JoinedAt && customer.Id > current.Id) {
			userCustomerMap[customer.UserId] = customer
		}
	}

	var scope *gorm.DB
	hasCustomerWithUser := false
	// ponytail: keep per-customer join boundaries with ORs; add task tenancy snapshots if customer counts make the query too large.
	for _, customer := range userCustomerMap {
		startBound := customer.JoinedAt
		if startTimestamp != nil && *startTimestamp > startBound {
			startBound = *startTimestamp
		}
		if scope == nil {
			scope = DB.Where("user_id = ? AND submit_time >= ?", customer.UserId, startBound)
		} else {
			scope = scope.Or("user_id = ? AND submit_time >= ?", customer.UserId, startBound)
		}
		hasCustomerWithUser = true
	}
	if !hasCustomerWithUser {
		return taskPage, nil
	}

	query := DB.Model(&Task{}).Where(scope)
	if endTimestamp != nil {
		query = query.Where("submit_time <= ?", *endTimestamp)
	}
	if taskID != nil {
		query = query.Where("task_id = ?", *taskID)
	}

	if err := query.Count(&taskPage.Total).Error; err != nil {
		return nil, err
	}

	rows := make([]resellerTaskRow, 0, pageSize)
	offset := (page - 1) * pageSize
	if err := query.Select("id, user_id, task_id, platform, action, status, progress, submit_time, start_time, finish_time, properties").
		Order("submit_time DESC").
		Order("id DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&rows).Error; err != nil {
		return nil, err
	}

	taskPage.Items = make([]ResellerTaskItem, 0, len(rows))
	for _, row := range rows {
		customer, ok := userCustomerMap[row.UserID]
		if !ok {
			continue
		}
		taskPage.Items = append(taskPage.Items, ResellerTaskItem{
			CustomerId:  customer.Id,
			TaskId:      row.TaskID,
			Platform:    string(row.Platform),
			Model:       strings.TrimSpace(row.Properties.OriginModelName),
			Action:      row.Action,
			Status:      string(row.Status),
			Progress:    row.Progress,
			SubmittedAt: row.SubmitTime,
			StartedAt:   row.StartTime,
			FinishedAt:  row.FinishTime,
		})
	}
	return taskPage, nil
}

func parseResellerUsageTokens(raw string) (int64, int64, int64) {
	if strings.TrimSpace(raw) == "" {
		return 0, 0, 0
	}
	var usage resellerTaskUsageJSON
	if err := common.UnmarshalJsonStr(raw, &usage); err != nil {
		return 0, 0, 0
	}
	promptTokens := usage.PromptTokens
	if promptTokens == 0 {
		promptTokens = usage.InputTokens
	}
	completionTokens := usage.CompletionTokens
	if completionTokens == 0 {
		completionTokens = usage.OutputTokens
	}
	totalTokens := usage.TotalTokens
	if totalTokens == 0 {
		totalTokens = promptTokens + completionTokens
	}
	return promptTokens, completionTokens, totalTokens
}
