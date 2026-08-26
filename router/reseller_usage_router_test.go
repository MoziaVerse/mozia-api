package router

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type resellerUsageEnvelopeData struct {
	Summary struct {
		RequestCount         string `json:"request_count"`
		PromptTokens         string `json:"prompt_tokens"`
		CompletionTokens     string `json:"completion_tokens"`
		TotalTokens          string `json:"total_tokens"`
		CustomerQuota        string `json:"customer_quota"`
		CustomerQuotaDisplay string `json:"customer_quota_display"`
		ModelCount           int    `json:"model_count"`
	} `json:"summary"`
	Items []struct {
		CustomerId           int    `json:"customer_id"`
		Model                string `json:"model"`
		RequestCount         string `json:"request_count"`
		PromptTokens         string `json:"prompt_tokens"`
		CompletionTokens     string `json:"completion_tokens"`
		TotalTokens          string `json:"total_tokens"`
		CustomerQuota        string `json:"customer_quota"`
		CustomerQuotaDisplay string `json:"customer_quota_display"`
	} `json:"items"`
	DailySpend []struct {
		Date          string `json:"date"`
		CustomerQuota string `json:"customer_quota"`
	} `json:"daily_spend"`
	CustomerSpend []struct {
		CustomerId           int    `json:"customer_id"`
		CustomerQuota        string `json:"customer_quota"`
		CustomerQuotaDisplay string `json:"customer_quota_display"`
	} `json:"customer_spend"`
	ModelSpend []struct {
		Model                string `json:"model"`
		CustomerQuota        string `json:"customer_quota"`
		CustomerQuotaDisplay string `json:"customer_quota_display"`
	} `json:"model_spend"`
	SubagentSpend []struct {
		SubagentMemberId     int    `json:"subagent_member_id"`
		RequestCount         string `json:"request_count"`
		TotalTokens          string `json:"total_tokens"`
		CustomerQuota        string `json:"customer_quota"`
		CustomerQuotaDisplay string `json:"customer_quota_display"`
	} `json:"subagent_spend"`
}

type resellerTasksEnvelopeData struct {
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Total    int64 `json:"total"`
	Items    []struct {
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
	} `json:"items"`
}

func TestResellerManagementUsageAndTasksContract(t *testing.T) {
	_, db, request := setupResellerM2Test(t)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Ability{}))

	resellerA := seedResellerM2(t, db, "Usage Agency A", "usage-a.example.com", model.ResellerRoleOwner, "usage-owner-a", "usage-admin-a", "usage-viewer-a")
	resellerB := seedResellerM2(t, db, "Usage Agency B", "usage-b.example.com", model.ResellerRoleOwner, "usage-owner-b", "usage-admin-b", "usage-viewer-b")
	customerA := seedCustomerM2(t, db, resellerA.Id, "usage-customer-a", model.ResellerCustomerStatusActive)
	customerC := seedCustomerM2(t, db, resellerA.Id, "usage-customer-c", model.ResellerCustomerStatusActive)
	customerD := seedCustomerM2(t, db, resellerA.Id, "usage-customer-d", model.ResellerCustomerStatusActive)
	customerB := seedCustomerM2(t, db, resellerB.Id, "usage-customer-b", model.ResellerCustomerStatusActive)
	customerNoUser := seedCustomerM2(t, db, resellerA.Id, "usage-customer-no-user", model.ResellerCustomerStatusActive)

	joinedAtA := int64(100)
	require.NoError(t, db.Model(&model.ResellerCustomer{}).Where("id = ?", customerA.Id).Update("created_at", joinedAtA).Error)
	require.NoError(t, db.Model(&model.ResellerCustomer{}).Where("id = ?", customerC.Id).Update("created_at", int64(125)).Error)
	require.NoError(t, db.Model(&model.ResellerCustomer{}).Where("id = ?", customerD.Id).Update("created_at", int64(135)).Error)
	require.NoError(t, db.Model(&model.ResellerCustomer{}).Where("id = ?", customerB.Id).Update("created_at", int64(200)).Error)
	require.NoError(t, db.Model(&model.ResellerCustomer{}).Where("id = ?", customerNoUser.Id).Update("created_at", int64(300)).Error)

	userA := model.User{
		Username:    "usage_user_a",
		Password:    "password123",
		DisplayName: "Usage User A",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Quota:       1000,
	}
	require.NoError(t, db.Create(&userA).Error)
	require.NoError(t, db.Create(&model.UserSSO{SSOSub: customerA.Subject, UserId: userA.Id}).Error)
	userC := model.User{
		Username:    "usage_user_c",
		Password:    "password123",
		DisplayName: "Usage User C",
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Quota:       1000,
		AffCode:     "usage-aff-c",
	}
	require.NoError(t, db.Create(&userC).Error)
	require.NoError(t, db.Create(&model.UserSSO{SSOSub: customerC.Subject, UserId: userC.Id}).Error)
	require.NoError(t, db.Create(&model.UserSSO{SSOSub: customerD.Subject, UserId: userC.Id}).Error)

	seedResellerSettlement(t, db, model.ResellerRequestSettlement{
		RequestId:               "usage-settlement-a1",
		ResellerId:              resellerA.Id,
		CustomerId:              customerA.Id,
		UserId:                  userA.Id,
		ModelName:               "alpha-model",
		WholesaleRuleId:         1,
		WholesaleRuleVersion:    1,
		WholesaleMultiplierPPM:  model.ResellerDefaultMultiplierPPM,
		RetailRuleId:            2,
		RetailRuleVersion:       1,
		RetailMultiplierPPM:     model.ResellerDefaultMultiplierPPM,
		ActualBaseQuota:         100,
		ActualCustomerQuota:     100,
		ActualWholesaleQuota:    100,
		UsageJSON:               `{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15,"seeded_sensitive":"wholesale-secret"}`,
		Status:                  model.ResellerSettlementStatusSettled,
		CreatedAt:               110,
		SettledAt:               111,
		EstimatedBaseQuota:      100,
		EstimatedCustomerQuota:  100,
		EstimatedWholesaleQuota: 100,
	})
	seedResellerSettlement(t, db, model.ResellerRequestSettlement{
		RequestId:               "usage-settlement-a2",
		ResellerId:              resellerA.Id,
		CustomerId:              customerA.Id,
		UserId:                  userA.Id,
		ModelName:               "alpha-model",
		WholesaleRuleId:         1,
		WholesaleRuleVersion:    1,
		WholesaleMultiplierPPM:  model.ResellerDefaultMultiplierPPM,
		RetailRuleId:            2,
		RetailRuleVersion:       1,
		RetailMultiplierPPM:     model.ResellerDefaultMultiplierPPM,
		ActualBaseQuota:         40,
		ActualCustomerQuota:     40,
		ActualWholesaleQuota:    40,
		UsageJSON:               `{"input_tokens":7,"output_tokens":3,"total_tokens":10,"upstream_model":"upstream-secret"}`,
		Status:                  model.ResellerSettlementStatusSettled,
		CreatedAt:               120,
		SettledAt:               121,
		EstimatedBaseQuota:      40,
		EstimatedCustomerQuota:  40,
		EstimatedWholesaleQuota: 40,
	})
	seedResellerSettlement(t, db, model.ResellerRequestSettlement{
		RequestId:               "usage-settlement-a3",
		ResellerId:              resellerA.Id,
		CustomerId:              customerA.Id,
		UserId:                  userA.Id,
		ModelName:               "beta-task-model",
		WholesaleRuleId:         1,
		WholesaleRuleVersion:    1,
		WholesaleMultiplierPPM:  model.ResellerDefaultMultiplierPPM,
		RetailRuleId:            2,
		RetailRuleVersion:       1,
		RetailMultiplierPPM:     model.ResellerDefaultMultiplierPPM,
		ActualBaseQuota:         60,
		ActualCustomerQuota:     60,
		ActualWholesaleQuota:    60,
		UsageJSON:               `{"kind":"task_tokens","total_tokens":99}`,
		Status:                  model.ResellerSettlementStatusSettled,
		CreatedAt:               130,
		SettledAt:               131,
		EstimatedBaseQuota:      60,
		EstimatedCustomerQuota:  60,
		EstimatedWholesaleQuota: 60,
	})
	seedResellerSettlement(t, db, model.ResellerRequestSettlement{
		RequestId:               "usage-settlement-refunded",
		ResellerId:              resellerA.Id,
		CustomerId:              customerA.Id,
		UserId:                  userA.Id,
		ModelName:               "alpha-model",
		WholesaleRuleId:         1,
		WholesaleRuleVersion:    1,
		WholesaleMultiplierPPM:  model.ResellerDefaultMultiplierPPM,
		RetailRuleId:            2,
		RetailRuleVersion:       1,
		RetailMultiplierPPM:     model.ResellerDefaultMultiplierPPM,
		ActualBaseQuota:         999,
		ActualCustomerQuota:     999,
		ActualWholesaleQuota:    999,
		UsageJSON:               `{"prompt_tokens":999,"completion_tokens":999,"total_tokens":1998,"seeded_sensitive":"refund-secret"}`,
		Status:                  model.ResellerSettlementStatusRefunded,
		CreatedAt:               125,
		RefundedAt:              126,
		EstimatedBaseQuota:      999,
		EstimatedCustomerQuota:  999,
		EstimatedWholesaleQuota: 999,
	})
	seedResellerSettlement(t, db, model.ResellerRequestSettlement{
		RequestId:               "usage-settlement-other-tenant",
		ResellerId:              resellerB.Id,
		CustomerId:              customerB.Id,
		UserId:                  999,
		ModelName:               "tenant-b-model",
		WholesaleRuleId:         1,
		WholesaleRuleVersion:    1,
		WholesaleMultiplierPPM:  model.ResellerDefaultMultiplierPPM,
		RetailRuleId:            2,
		RetailRuleVersion:       1,
		RetailMultiplierPPM:     model.ResellerDefaultMultiplierPPM,
		ActualBaseQuota:         88,
		ActualCustomerQuota:     88,
		ActualWholesaleQuota:    88,
		UsageJSON:               `{"total_tokens":88}`,
		Status:                  model.ResellerSettlementStatusSettled,
		CreatedAt:               210,
		SettledAt:               211,
		EstimatedBaseQuota:      88,
		EstimatedCustomerQuota:  88,
		EstimatedWholesaleQuota: 88,
	})
	seedResellerSettlement(t, db, model.ResellerRequestSettlement{
		RequestId:               "usage-settlement-second-customer",
		ResellerId:              resellerA.Id,
		CustomerId:              customerNoUser.Id,
		UserId:                  userA.Id,
		ModelName:               "alpha-model",
		WholesaleRuleId:         1,
		WholesaleRuleVersion:    1,
		WholesaleMultiplierPPM:  model.ResellerDefaultMultiplierPPM,
		RetailRuleId:            2,
		RetailRuleVersion:       1,
		RetailMultiplierPPM:     model.ResellerDefaultMultiplierPPM,
		ActualBaseQuota:         10,
		ActualCustomerQuota:     10,
		ActualWholesaleQuota:    10,
		UsageJSON:               `{"total_tokens":10}`,
		Status:                  model.ResellerSettlementStatusSettled,
		CreatedAt:               135,
		SettledAt:               136,
		EstimatedBaseQuota:      10,
		EstimatedCustomerQuota:  10,
		EstimatedWholesaleQuota: 10,
	})

	seedTask(t, db, model.Task{
		TaskID:      "task-before-join",
		UserId:      userA.Id,
		Action:      "generate",
		Status:      model.TaskStatusFailure,
		Progress:    "100%",
		SubmitTime:  90,
		StartTime:   91,
		FinishTime:  92,
		Properties:  model.Properties{OriginModelName: "model-before-join", UpstreamModelName: "hidden-upstream-model", Input: "hidden-input"},
		FailReason:  "hidden-fail-reason",
		PrivateData: model.TaskPrivateData{Key: "hidden-key", UpstreamTaskID: "hidden-upstream-id", ResultURL: "https://hidden.example/result-before"},
		Data:        []byte(`{"result_url":"hidden-result-url-before"}`),
	})
	seedTask(t, db, model.Task{
		TaskID:      "task-after-120",
		Platform:    "veo",
		UserId:      userA.Id,
		Action:      "generate",
		Status:      model.TaskStatusSuccess,
		Progress:    "100%",
		SubmitTime:  120,
		StartTime:   121,
		FinishTime:  125,
		Properties:  model.Properties{OriginModelName: "safe-model-a", UpstreamModelName: "hidden-upstream-model", Input: "hidden-input"},
		FailReason:  "hidden-fail-reason",
		PrivateData: model.TaskPrivateData{Key: "hidden-key", UpstreamTaskID: "hidden-upstream-id", ResultURL: "https://hidden.example/result-a"},
		Data:        []byte(`{"result_url":"hidden-result-url-a"}`),
	})
	seedTask(t, db, model.Task{
		TaskID:      "task-after-130",
		Platform:    "kling",
		UserId:      userA.Id,
		Action:      "remixGenerate",
		Status:      model.TaskStatusInProgress,
		Progress:    "55%",
		SubmitTime:  130,
		StartTime:   131,
		FinishTime:  0,
		Properties:  model.Properties{OriginModelName: "safe-model-b", UpstreamModelName: "hidden-upstream-model", Input: "hidden-input"},
		FailReason:  "hidden-fail-reason",
		PrivateData: model.TaskPrivateData{Key: "hidden-key", UpstreamTaskID: "hidden-upstream-id", ResultURL: "https://hidden.example/result-b"},
		Data:        []byte(`{"result_url":"hidden-result-url-b"}`),
	})
	seedTask(t, db, model.Task{
		TaskID:      "task-after-140",
		Platform:    "suno",
		UserId:      userA.Id,
		Action:      "generate",
		Status:      model.TaskStatusSubmitted,
		Progress:    "0%",
		SubmitTime:  140,
		StartTime:   0,
		FinishTime:  0,
		Properties:  model.Properties{OriginModelName: "safe-model-c", UpstreamModelName: "hidden-upstream-model", Input: "hidden-input"},
		FailReason:  "hidden-fail-reason",
		PrivateData: model.TaskPrivateData{Key: "hidden-key", UpstreamTaskID: "hidden-upstream-id", ResultURL: "https://hidden.example/result-c"},
		Data:        []byte(`{"result_url":"hidden-result-url-c"}`),
	})
	seedTask(t, db, model.Task{
		TaskID:      "task-c-before-join",
		Platform:    "veo",
		UserId:      userC.Id,
		Action:      "generate",
		Status:      model.TaskStatusFailure,
		Progress:    "100%",
		SubmitTime:  124,
		StartTime:   124,
		FinishTime:  124,
		Properties:  model.Properties{OriginModelName: "customer-c-before", UpstreamModelName: "hidden-upstream-model", Input: "hidden-input"},
		FailReason:  "hidden-fail-reason",
		PrivateData: model.TaskPrivateData{Key: "hidden-key", UpstreamTaskID: "hidden-upstream-id", ResultURL: "https://hidden.example/result-c-before"},
		Data:        []byte(`{"result_url":"hidden-result-url-c-before"}`),
	})
	seedTask(t, db, model.Task{
		TaskID:      "task-c-after-128",
		Platform:    "kling",
		UserId:      userC.Id,
		Action:      "remixGenerate",
		Status:      model.TaskStatusSuccess,
		Progress:    "100%",
		SubmitTime:  128,
		StartTime:   128,
		FinishTime:  129,
		Properties:  model.Properties{OriginModelName: "customer-c-model-a", UpstreamModelName: "hidden-upstream-model", Input: "hidden-input"},
		FailReason:  "hidden-fail-reason",
		PrivateData: model.TaskPrivateData{Key: "hidden-key", UpstreamTaskID: "hidden-upstream-id", ResultURL: "https://hidden.example/result-c-128"},
		Data:        []byte(`{"result_url":"hidden-result-url-c-128"}`),
	})
	seedTask(t, db, model.Task{
		TaskID:      "task-c-after-150",
		Platform:    "suno",
		UserId:      userC.Id,
		Action:      "generate",
		Status:      model.TaskStatusSubmitted,
		Progress:    "15%",
		SubmitTime:  150,
		StartTime:   0,
		FinishTime:  0,
		Properties:  model.Properties{OriginModelName: "customer-c-model-b", UpstreamModelName: "hidden-upstream-model", Input: "hidden-input"},
		FailReason:  "hidden-fail-reason",
		PrivateData: model.TaskPrivateData{Key: "hidden-key", UpstreamTaskID: "hidden-upstream-id", ResultURL: "https://hidden.example/result-c-150"},
		Data:        []byte(`{"result_url":"hidden-result-url-c-150"}`),
	})

	viewerHeaders := map[string]string{
		"X-Reseller-Subject": "usage-viewer-a",
		"X-Reseller-Host":    "usage-a.example.com",
	}
	adminHeaders := map[string]string{
		"X-Reseller-Subject": "usage-admin-a",
		"X-Reseller-Host":    "usage-a.example.com",
	}
	ownerHeaders := map[string]string{
		"X-Reseller-Subject": "usage-owner-a",
		"X-Reseller-Host":    "usage-a.example.com",
	}
	crossTenantSubagent := request(http.MethodPost, "/api/internal/v1/reseller/management/members/subagents", fmt.Sprintf(`{"name":"越权客户组","customer_id":%d}`, customerB.Id), "matrix-reseller-management-test-token", "subagent-cross-tenant_123", ownerHeaders)
	require.Equal(t, http.StatusNotFound, crossTenantSubagent.Code)
	createSubagent := request(http.MethodPost, "/api/internal/v1/reseller/management/members/subagents", fmt.Sprintf(`{"name":"华东客户组","customer_id":%d}`, customerD.Id), "matrix-reseller-management-test-token", "subagent-create_123", ownerHeaders)
	require.Equal(t, http.StatusCreated, createSubagent.Code)
	createSubagentEnvelope := decodeM2Envelope(t, createSubagent)
	var subagent model.ResellerMemberRecord
	require.NoError(t, common.Unmarshal(createSubagentEnvelope.RawData, &subagent))
	assert.Equal(t, "华东客户组", subagent.Name)
	assert.Equal(t, customerD.Subject, subagent.Subject)
	selfAssign := request(http.MethodPatch, fmt.Sprintf("/api/internal/v1/reseller/management/customers/%d/subagent", customerD.Id), fmt.Sprintf(`{"subagent_member_id":%d}`, subagent.Id), "matrix-reseller-management-test-token", "subagent-self-assign_123", ownerHeaders)
	require.Equal(t, http.StatusConflict, selfAssign.Code)
	assignAdministratorCandidate := request(http.MethodPatch, fmt.Sprintf("/api/internal/v1/reseller/management/customers/%d/subagent", customerNoUser.Id), fmt.Sprintf(`{"subagent_member_id":%d}`, subagent.Id), "matrix-reseller-management-test-token", "subagent-assign-candidate_123", ownerHeaders)
	require.Equal(t, http.StatusOK, assignAdministratorCandidate.Code)
	createFromAssignedCustomer := request(http.MethodPost, "/api/internal/v1/reseller/management/members/subagents", fmt.Sprintf(`{"name":"冲突客户组","customer_id":%d}`, customerNoUser.Id), "matrix-reseller-management-test-token", "subagent-create-assigned_123", ownerHeaders)
	require.Equal(t, http.StatusConflict, createFromAssignedCustomer.Code)
	clearAdministratorCandidate := request(http.MethodPatch, fmt.Sprintf("/api/internal/v1/reseller/management/customers/%d/subagent", customerNoUser.Id), `{"subagent_member_id":0}`, "matrix-reseller-management-test-token", "subagent-clear-candidate_123", ownerHeaders)
	require.Equal(t, http.StatusOK, clearAdministratorCandidate.Code)
	assignSubagent := request(http.MethodPatch, fmt.Sprintf("/api/internal/v1/reseller/management/customers/%d/subagent", customerA.Id), fmt.Sprintf(`{"subagent_member_id":%d}`, subagent.Id), "matrix-reseller-management-test-token", "subagent-assign_123", ownerHeaders)
	require.Equal(t, http.StatusOK, assignSubagent.Code)
	require.NoError(t, db.Model(&model.ResellerCustomer{}).Where("id = ?", customerA.Id).Updates(map[string]any{
		"subagent_member_id":   subagent.Id,
		"subagent_assigned_at": int64(115),
	}).Error)
	subagentHeaders := map[string]string{
		"X-Reseller-Subject": subagent.Subject,
		"X-Reseller-Host":    "usage-a.example.com",
	}

	t.Run("usage requires management token and tenant headers", func(t *testing.T) {
		unauthorized := request(http.MethodGet, "/api/internal/v1/reseller/management/usage", "", "matrix-reseller-test-token", "usage-auth_123", viewerHeaders)
		unauthorizedEnvelope := decodeM2Envelope(t, unauthorized)
		require.Equal(t, http.StatusUnauthorized, unauthorized.Code)
		assert.Equal(t, middleware.ResellerErrorServiceUnauthorized, unauthorizedEnvelope.Error.Code)

		missingHeaders := request(http.MethodGet, "/api/internal/v1/reseller/management/usage", "", "matrix-reseller-management-test-token", "usage-missing-headers_123", nil)
		missingHeadersEnvelope := decodeM2Envelope(t, missingHeaders)
		require.Equal(t, http.StatusBadRequest, missingHeaders.Code)
		assert.Equal(t, middleware.ResellerErrorInvalidRequest, missingHeadersEnvelope.Error.Code)
	})

	t.Run("subagent sees only assigned customers and post-assignment usage", func(t *testing.T) {
		customers := request(http.MethodGet, "/api/internal/v1/reseller/management/customers", "", "matrix-reseller-management-test-token", "subagent-customers_123", subagentHeaders)
		require.Equal(t, http.StatusOK, customers.Code)
		customersEnvelope := decodeM2Envelope(t, customers)
		var scopedCustomers []resellerM2Customer
		require.NoError(t, common.Unmarshal(customersEnvelope.RawData, &scopedCustomers))
		require.Len(t, scopedCustomers, 1)
		assert.Equal(t, customerA.Id, scopedCustomers[0].Id)

		usage := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/reseller/management/usage?customer_id=%d&start_timestamp=100&end_timestamp=140", customerA.Id), "", "matrix-reseller-management-test-token", "subagent-usage_123", subagentHeaders)
		require.Equal(t, http.StatusOK, usage.Code)
		usageEnvelope := decodeM2Envelope(t, usage)
		var data resellerUsageEnvelopeData
		require.NoError(t, common.Unmarshal(usageEnvelope.RawData, &data))
		assert.Equal(t, "2", data.Summary.RequestCount)
		assert.Equal(t, "100", data.Summary.CustomerQuota)

		ownerUsage := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/reseller/management/usage?subagent_member_id=%d&start_timestamp=100&end_timestamp=140", subagent.Id), "", "matrix-reseller-management-test-token", "owner-subagent-usage_123", ownerHeaders)
		require.Equal(t, http.StatusOK, ownerUsage.Code)
		ownerUsageEnvelope := decodeM2Envelope(t, ownerUsage)
		var ownerUsageData resellerUsageEnvelopeData
		require.NoError(t, common.Unmarshal(ownerUsageEnvelope.RawData, &ownerUsageData))
		assert.Equal(t, "2", ownerUsageData.Summary.RequestCount)
		assert.Equal(t, "100", ownerUsageData.Summary.CustomerQuota)
		require.Len(t, ownerUsageData.SubagentSpend, 1)
		assert.Equal(t, subagent.Id, ownerUsageData.SubagentSpend[0].SubagentMemberId)
		assert.Equal(t, "2", ownerUsageData.SubagentSpend[0].RequestCount)

		ownerTasks := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/reseller/management/tasks?subagent_member_id=%d&start_timestamp=100&end_timestamp=140", subagent.Id), "", "matrix-reseller-management-test-token", "owner-subagent-tasks_123", ownerHeaders)
		require.Equal(t, http.StatusOK, ownerTasks.Code)
		ownerTasksEnvelope := decodeM2Envelope(t, ownerTasks)
		var ownerTasksData resellerTasksEnvelopeData
		require.NoError(t, common.Unmarshal(ownerTasksEnvelope.RawData, &ownerTasksData))
		assert.Equal(t, int64(3), ownerTasksData.Total)

		missingSubagent := request(http.MethodGet, "/api/internal/v1/reseller/management/usage?subagent_member_id=999999", "", "matrix-reseller-management-test-token", "owner-missing-subagent_123", ownerHeaders)
		require.Equal(t, http.StatusNotFound, missingSubagent.Code)

		for _, path := range []string{
			fmt.Sprintf("/api/internal/v1/reseller/management/usage?customer_id=%d", customerC.Id),
			"/api/internal/v1/reseller/management/pricing",
			"/api/internal/v1/reseller/management/members",
			"/api/internal/v1/reseller/management/invitations",
		} {
			recorder := request(http.MethodGet, path, "", "matrix-reseller-management-test-token", "subagent-forbidden_123", subagentHeaders)
			assert.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, recorder.Code)
		}

		enableCapabilities := request(http.MethodPatch, fmt.Sprintf("/api/internal/v1/reseller/management/members/subagents/%d/capabilities", subagent.Id), `{"can_manage_pricing":true,"can_create_invitations":true}`, "matrix-reseller-management-test-token", "subagent-capabilities_123", ownerHeaders)
		require.Equal(t, http.StatusOK, enableCapabilities.Code)

		context := request(http.MethodPost, "/api/internal/v1/reseller/context", fmt.Sprintf(`{"subject":%q,"host":"usage-a.example.com"}`, subagent.Subject), "matrix-reseller-test-token", "subagent-context_123", nil)
		contextEnvelope := decodeM2Envelope(t, context)
		var contextData resellerM2Profile
		require.NoError(t, common.Unmarshal(contextEnvelope.RawData, &contextData))
		assert.Contains(t, contextData.Permissions, "reseller:pricing:write")
		assert.Contains(t, contextData.Permissions, "reseller:invitations:write")

		_, err := model.CreateResellerPriceRule(model.CreateResellerPriceRuleParams{
			ResellerId: resellerA.Id, Kind: model.ResellerPriceRuleKindWholesale, ModelName: "subagent-model",
			MultiplierPPM: 800000, Enabled: true, EffectiveAt: common.GetTimestamp(), CreatedBy: "platform",
		})
		require.NoError(t, err)
		pricing := request(http.MethodGet, "/api/internal/v1/reseller/management/pricing", "", "matrix-reseller-management-test-token", "subagent-pricing_123", subagentHeaders)
		require.Equal(t, http.StatusOK, pricing.Code)
		pricingEnvelope := decodeM2Envelope(t, pricing)
		var pricingData struct {
			Rules []model.ResellerPriceRuleRecord `json:"rules"`
		}
		require.NoError(t, common.Unmarshal(pricingEnvelope.RawData, &pricingData))
		require.NotEmpty(t, pricingData.Rules)
		assert.Equal(t, model.ResellerPriceRuleKindWholesale, pricingData.Rules[0].Kind)
		assert.Equal(t, "subagent-model", pricingData.Rules[0].Model)
		assert.Equal(t, "0.8", pricingData.Rules[0].Multiplier)

		retail := request(http.MethodPost, "/api/internal/v1/reseller/management/pricing/retail", fmt.Sprintf(`{"model":"subagent-model","multiplier":"1.2","customer_id":%d}`, customerA.Id), "matrix-reseller-management-test-token", "subagent-retail_123", subagentHeaders)
		require.Equal(t, http.StatusCreated, retail.Code)
		forgedRetail := request(http.MethodPost, "/api/internal/v1/reseller/management/pricing/retail", fmt.Sprintf(`{"model":"subagent-model","multiplier":"1.2","customer_id":%d}`, customerD.Id), "matrix-reseller-management-test-token", "subagent-retail-forged_123", subagentHeaders)
		require.Equal(t, http.StatusNotFound, forgedRetail.Code)

		createdInvitationRecorder := request(http.MethodPost, "/api/internal/v1/reseller/management/invitations", `{"expires_in_hours":24}`, "matrix-reseller-management-test-token", "subagent-invite_123", subagentHeaders)
		createdInvitationEnvelope := decodeM2Envelope(t, createdInvitationRecorder)
		var createdInvitation resellerM2InvitationCreate
		require.NoError(t, common.Unmarshal(createdInvitationEnvelope.RawData, &createdInvitation))
		require.Equal(t, http.StatusCreated, createdInvitationRecorder.Code)
		require.NotNil(t, createdInvitation.Invitation.SubagentMemberId)
		assert.Equal(t, subagent.Id, *createdInvitation.Invitation.SubagentMemberId)

		consumeInvitation := request(http.MethodPost, "/api/internal/v1/reseller/registration/invitations/consume", fmt.Sprintf(`{"token":%q,"subject":"subagent-invited-customer","matrix_name":"随机客户","phone":"13700137000"}`, createdInvitation.Token), "matrix-reseller-registration-test-token", "subagent-invite-consume_123", nil)
		consumeInvitationEnvelope := decodeM2Envelope(t, consumeInvitation)
		var consumed resellerM2Consume
		require.NoError(t, common.Unmarshal(consumeInvitationEnvelope.RawData, &consumed))
		require.Equal(t, http.StatusCreated, consumeInvitation.Code)
		require.NotNil(t, consumed.Customer.SubagentMemberId)
		assert.Equal(t, subagent.Id, *consumed.Customer.SubagentMemberId)
		assert.Positive(t, consumed.Customer.SubagentAssignedAt)

		pendingInvitation := request(http.MethodPost, "/api/internal/v1/reseller/management/invitations", `{"expires_in_hours":24}`, "matrix-reseller-management-test-token", "subagent-invite-pending_123", subagentHeaders)
		pendingInvitationEnvelope := decodeM2Envelope(t, pendingInvitation)
		var pendingInvitationData resellerM2InvitationCreate
		require.NoError(t, common.Unmarshal(pendingInvitationEnvelope.RawData, &pendingInvitationData))
		require.Equal(t, http.StatusCreated, pendingInvitation.Code)
		disableInvitations := request(http.MethodPatch, fmt.Sprintf("/api/internal/v1/reseller/management/members/subagents/%d/capabilities", subagent.Id), `{"can_manage_pricing":true,"can_create_invitations":false}`, "matrix-reseller-management-test-token", "subagent-disable-invites_123", ownerHeaders)
		require.Equal(t, http.StatusOK, disableInvitations.Code)
		forbiddenInvitation := request(http.MethodPost, "/api/internal/v1/reseller/management/invitations", `{"expires_in_hours":24}`, "matrix-reseller-management-test-token", "subagent-invite-forbidden_123", subagentHeaders)
		require.Equal(t, http.StatusForbidden, forbiddenInvitation.Code)
		blockedConsume := request(http.MethodPost, "/api/internal/v1/reseller/registration/invitations/consume", fmt.Sprintf(`{"token":%q,"subject":"blocked-subagent-customer"}`, pendingInvitationData.Token), "matrix-reseller-registration-test-token", "subagent-invite-blocked-consume_123", nil)
		require.Equal(t, http.StatusConflict, blockedConsume.Code)

		cleared := request(http.MethodPatch, fmt.Sprintf("/api/internal/v1/reseller/management/customers/%d/subagent", customerA.Id), `{"subagent_member_id":0}`, "matrix-reseller-management-test-token", "subagent-clear_123", ownerHeaders)
		require.Equal(t, http.StatusOK, cleared.Code)
		clearedInvited := request(http.MethodPatch, fmt.Sprintf("/api/internal/v1/reseller/management/customers/%d/subagent", consumed.Customer.Id), `{"subagent_member_id":0}`, "matrix-reseller-management-test-token", "subagent-clear-invited_123", ownerHeaders)
		require.Equal(t, http.StatusOK, clearedInvited.Code)
		customers = request(http.MethodGet, "/api/internal/v1/reseller/management/customers", "", "matrix-reseller-management-test-token", "subagent-customers-empty_123", subagentHeaders)
		customersEnvelope = decodeM2Envelope(t, customers)
		require.NoError(t, common.Unmarshal(customersEnvelope.RawData, &scopedCustomers))
		assert.Empty(t, scopedCustomers)
	})

	t.Run("usage aggregates settled rows only with string counters and no sensitive leakage", func(t *testing.T) {
		recorder := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/reseller/management/usage?customer_id=%d&start_timestamp=100&end_timestamp=140", customerA.Id), "", "matrix-reseller-management-test-token", "usage-success_123", viewerHeaders)
		envelope := decodeM2Envelope(t, recorder)
		require.Equal(t, http.StatusOK, recorder.Code)

		var data resellerUsageEnvelopeData
		require.NoError(t, common.Unmarshal(envelope.RawData, &data))
		assert.Equal(t, "3", data.Summary.RequestCount)
		assert.Equal(t, "17", data.Summary.PromptTokens)
		assert.Equal(t, "8", data.Summary.CompletionTokens)
		assert.Equal(t, "124", data.Summary.TotalTokens)
		assert.Equal(t, "200", data.Summary.CustomerQuota)
		assert.Equal(t, logger.FormatQuota(200), data.Summary.CustomerQuotaDisplay)
		assert.Equal(t, 2, data.Summary.ModelCount)
		require.Len(t, data.DailySpend, 1)
		assert.Equal(t, "1970-01-01", data.DailySpend[0].Date)
		assert.Equal(t, "200", data.DailySpend[0].CustomerQuota)
		require.Len(t, data.Items, 2)
		assert.Equal(t, []string{"alpha-model", "beta-task-model"}, []string{data.Items[0].Model, data.Items[1].Model})
		assert.Equal(t, "2", data.Items[0].RequestCount)
		assert.Equal(t, "17", data.Items[0].PromptTokens)
		assert.Equal(t, "8", data.Items[0].CompletionTokens)
		assert.Equal(t, "25", data.Items[0].TotalTokens)
		assert.Equal(t, "140", data.Items[0].CustomerQuota)
		assert.Equal(t, "1", data.Items[1].RequestCount)
		assert.Equal(t, "0", data.Items[1].PromptTokens)
		assert.Equal(t, "0", data.Items[1].CompletionTokens)
		assert.Equal(t, "99", data.Items[1].TotalTokens)
		assert.Equal(t, "60", data.Items[1].CustomerQuota)
		require.Len(t, data.CustomerSpend, 1)
		assert.Equal(t, customerA.Id, data.CustomerSpend[0].CustomerId)
		assert.Equal(t, "200", data.CustomerSpend[0].CustomerQuota)
		assert.Equal(t, logger.FormatQuota(200), data.CustomerSpend[0].CustomerQuotaDisplay)
		require.Len(t, data.ModelSpend, 2)
		assert.Equal(t, []string{"alpha-model", "beta-task-model"}, []string{data.ModelSpend[0].Model, data.ModelSpend[1].Model})
		assert.Equal(t, []string{"140", "60"}, []string{data.ModelSpend[0].CustomerQuota, data.ModelSpend[1].CustomerQuota})
		assert.Equal(t, logger.FormatQuota(140), data.ModelSpend[0].CustomerQuotaDisplay)

		body := recorder.Body.String()
		assert.NotContains(t, body, "wholesale-secret")
		assert.NotContains(t, body, "upstream-secret")
		assert.NotContains(t, body, "refund-secret")

		modelRecorder := request(http.MethodGet, "/api/internal/v1/reseller/management/usage?start_timestamp=100&end_timestamp=140&model=alpha-model", "", "matrix-reseller-management-test-token", "usage-model_123", viewerHeaders)
		modelEnvelope := decodeM2Envelope(t, modelRecorder)
		require.Equal(t, http.StatusOK, modelRecorder.Code)
		var modelData resellerUsageEnvelopeData
		require.NoError(t, common.Unmarshal(modelEnvelope.RawData, &modelData))
		assert.Equal(t, 1, modelData.Summary.ModelCount)
		require.Len(t, modelData.Items, 2)
		assert.Equal(t, []string{"alpha-model", "alpha-model"}, []string{modelData.Items[0].Model, modelData.Items[1].Model})

		allRecorder := request(http.MethodGet, "/api/internal/v1/reseller/management/usage?start_timestamp=100&end_timestamp=140", "", "matrix-reseller-management-test-token", "usage-all_123", viewerHeaders)
		allEnvelope := decodeM2Envelope(t, allRecorder)
		require.Equal(t, http.StatusOK, allRecorder.Code)
		var allData resellerUsageEnvelopeData
		require.NoError(t, common.Unmarshal(allEnvelope.RawData, &allData))
		assert.Equal(t, 2, allData.Summary.ModelCount, "model_count is distinct across customers")
		require.Len(t, allData.Items, 3)
	})

	t.Run("platform usage uses the dedicated mega token and requested reseller scope", func(t *testing.T) {
		unauthorized := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/platform/resellers/%d/usage", resellerA.Id), "", "matrix-reseller-management-test-token", "platform-usage-auth_123", nil)
		require.Equal(t, http.StatusUnauthorized, unauthorized.Code)

		recorder := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/platform/resellers/%d/usage?start_timestamp=100&end_timestamp=140", resellerA.Id), "", "mozia-mega-test-token", "platform-usage_123", nil)
		envelope := decodeM2Envelope(t, recorder)
		require.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, "platform-usage_123", envelope.RequestId)
		var data resellerUsageEnvelopeData
		require.NoError(t, common.Unmarshal(envelope.RawData, &data))
		assert.Equal(t, "4", data.Summary.RequestCount)
		assert.Equal(t, "210", data.Summary.CustomerQuota)
		require.Len(t, data.CustomerSpend, 2)

		crossTenantCustomer := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/platform/resellers/%d/usage?customer_id=%d", resellerA.Id, customerB.Id), "", "mozia-mega-test-token", "platform-usage-cross-tenant_123", nil)
		require.Equal(t, http.StatusNotFound, crossTenantCustomer.Code)
	})

	t.Run("usage enforces tenant scope forged reseller id and valid time range", func(t *testing.T) {
		crossTenant := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/reseller/management/usage?customer_id=%d", customerB.Id), "", "matrix-reseller-management-test-token", "usage-cross-tenant_123", ownerHeaders)
		crossTenantEnvelope := decodeM2Envelope(t, crossTenant)
		require.Equal(t, http.StatusNotFound, crossTenant.Code)
		assert.Equal(t, middleware.ResellerErrorNotFound, crossTenantEnvelope.Error.Code)

		forged := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/reseller/management/usage?customer_id=%d&reseller_id=%d", customerA.Id, resellerB.Id), "", "matrix-reseller-management-test-token", "usage-forged_123", ownerHeaders)
		forgedEnvelope := decodeM2Envelope(t, forged)
		require.Equal(t, http.StatusBadRequest, forged.Code)
		assert.Equal(t, middleware.ResellerErrorInvalidRequest, forgedEnvelope.Error.Code)

		for _, path := range []string{
			fmt.Sprintf("/api/internal/v1/reseller/management/usage?customer_id=%d&start_timestamp=100", customerA.Id),
			fmt.Sprintf("/api/internal/v1/reseller/management/usage?customer_id=%d&start_timestamp=140&end_timestamp=100", customerA.Id),
			fmt.Sprintf("/api/internal/v1/reseller/management/usage?customer_id=%d&start_timestamp=0&end_timestamp=100", customerA.Id),
			fmt.Sprintf("/api/internal/v1/reseller/management/usage?customer_id=%d&model=alpha-model&model=beta-task-model", customerA.Id),
		} {
			recorder := request(http.MethodGet, path, "", "matrix-reseller-management-test-token", "usage-invalid-time_123", ownerHeaders)
			envelope := decodeM2Envelope(t, recorder)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Equal(t, middleware.ResellerErrorInvalidRequest, envelope.Error.Code)
		}
	})

	t.Run("tasks honor joined_at time fence pagination and whitelist fields", func(t *testing.T) {
		recorder := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/reseller/management/tasks?customer_id=%d&start_timestamp=80&end_timestamp=130", customerA.Id), "", "matrix-reseller-management-test-token", "tasks-range_123", adminHeaders)
		envelope := decodeM2Envelope(t, recorder)
		require.Equal(t, http.StatusOK, recorder.Code)

		var data resellerTasksEnvelopeData
		require.NoError(t, common.Unmarshal(envelope.RawData, &data))
		assert.Equal(t, 1, data.Page)
		assert.Equal(t, 20, data.PageSize)
		assert.Equal(t, int64(2), data.Total)
		require.Len(t, data.Items, 2)
		assert.Equal(t, []string{"task-after-130", "task-after-120"}, []string{data.Items[0].TaskId, data.Items[1].TaskId})
		assert.Equal(t, []string{"kling", "veo"}, []string{data.Items[0].Platform, data.Items[1].Platform})
		assert.Equal(t, []string{"safe-model-b", "safe-model-a"}, []string{data.Items[0].Model, data.Items[1].Model})
		assert.Equal(t, []int64{130, 120}, []int64{data.Items[0].SubmittedAt, data.Items[1].SubmittedAt})

		body := recorder.Body.String()
		for _, hidden := range []string{
			"hidden-key",
			"hidden-upstream-id",
			"hidden-upstream-model",
			"hidden-input",
			"hidden-fail-reason",
			"hidden-result-url",
		} {
			assert.NotContains(t, body, hidden)
		}

		pageTwo := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/reseller/management/tasks?customer_id=%d&p=2&page_size=1", customerA.Id), "", "matrix-reseller-management-test-token", "tasks-page-two_123", adminHeaders)
		pageTwoEnvelope := decodeM2Envelope(t, pageTwo)
		require.Equal(t, http.StatusOK, pageTwo.Code)
		var pageTwoData resellerTasksEnvelopeData
		require.NoError(t, common.Unmarshal(pageTwoEnvelope.RawData, &pageTwoData))
		assert.Equal(t, 2, pageTwoData.Page)
		assert.Equal(t, 1, pageTwoData.PageSize)
		assert.Equal(t, int64(3), pageTwoData.Total)
		require.Len(t, pageTwoData.Items, 1)
		assert.Equal(t, "task-after-130", pageTwoData.Items[0].TaskId)

		filtered := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/reseller/management/tasks?customer_id=%d&task_id=task-after-120", customerA.Id), "", "matrix-reseller-management-test-token", "tasks-filtered_123", adminHeaders)
		filteredEnvelope := decodeM2Envelope(t, filtered)
		require.Equal(t, http.StatusOK, filtered.Code)
		var filteredData resellerTasksEnvelopeData
		require.NoError(t, common.Unmarshal(filteredEnvelope.RawData, &filteredData))
		assert.Equal(t, int64(1), filteredData.Total)
		require.Len(t, filteredData.Items, 1)
		assert.Equal(t, "task-after-120", filteredData.Items[0].TaskId)
	})

	t.Run("tasks without customer_id merge current reseller customers with global ordering and pagination", func(t *testing.T) {
		all := request(http.MethodGet, "/api/internal/v1/reseller/management/tasks?start_timestamp=80&end_timestamp=160", "", "matrix-reseller-management-test-token", "tasks-all_123", adminHeaders)
		allEnvelope := decodeM2Envelope(t, all)
		require.Equal(t, http.StatusOK, all.Code)

		var allData resellerTasksEnvelopeData
		require.NoError(t, common.Unmarshal(allEnvelope.RawData, &allData))
		assert.Equal(t, 1, allData.Page)
		assert.Equal(t, 20, allData.PageSize)
		assert.Equal(t, int64(4), allData.Total)
		require.Len(t, allData.Items, 4)
		assert.Equal(t,
			[]string{"task-c-after-150", "task-after-140", "task-after-130", "task-after-120"},
			[]string{allData.Items[0].TaskId, allData.Items[1].TaskId, allData.Items[2].TaskId, allData.Items[3].TaskId},
		)
		assert.Equal(t,
			[]int{customerD.Id, customerA.Id, customerA.Id, customerA.Id},
			[]int{allData.Items[0].CustomerId, allData.Items[1].CustomerId, allData.Items[2].CustomerId, allData.Items[3].CustomerId},
		)

		pageTwo := request(http.MethodGet, "/api/internal/v1/reseller/management/tasks?start_timestamp=80&end_timestamp=160&p=2&page_size=2", "", "matrix-reseller-management-test-token", "tasks-all-page-two_123", adminHeaders)
		pageTwoEnvelope := decodeM2Envelope(t, pageTwo)
		require.Equal(t, http.StatusOK, pageTwo.Code)
		var pageTwoData resellerTasksEnvelopeData
		require.NoError(t, common.Unmarshal(pageTwoEnvelope.RawData, &pageTwoData))
		assert.Equal(t, 2, pageTwoData.Page)
		assert.Equal(t, 2, pageTwoData.PageSize)
		assert.Equal(t, int64(4), pageTwoData.Total)
		require.Len(t, pageTwoData.Items, 2)
		assert.Equal(t, []string{"task-after-130", "task-after-120"}, []string{pageTwoData.Items[0].TaskId, pageTwoData.Items[1].TaskId})
		assert.Equal(t, []int{customerA.Id, customerA.Id}, []int{pageTwoData.Items[0].CustomerId, pageTwoData.Items[1].CustomerId})
	})

	t.Run("tasks without customer_id deduplicate shared user to latest customer boundary", func(t *testing.T) {
		recorder := request(http.MethodGet, "/api/internal/v1/reseller/management/tasks?start_timestamp=80&end_timestamp=160", "", "matrix-reseller-management-test-token", "tasks-shared-user_123", adminHeaders)
		envelope := decodeM2Envelope(t, recorder)
		require.Equal(t, http.StatusOK, recorder.Code)

		var data resellerTasksEnvelopeData
		require.NoError(t, common.Unmarshal(envelope.RawData, &data))
		for _, item := range data.Items {
			assert.NotEqual(t, "task-c-after-128", item.TaskId)
		}

		var latestCustomerItems []string
		for _, item := range data.Items {
			if item.CustomerId == customerD.Id {
				latestCustomerItems = append(latestCustomerItems, item.TaskId)
			}
		}
		assert.Equal(t, []string{"task-c-after-150"}, latestCustomerItems)
	})

	t.Run("tasks reject invalid input hide cross tenant data and return empty page for unbound user", func(t *testing.T) {
		for _, path := range []string{
			fmt.Sprintf("/api/internal/v1/reseller/management/tasks?customer_id=%d&start_timestamp=100", customerA.Id),
			fmt.Sprintf("/api/internal/v1/reseller/management/tasks?customer_id=%d&start_timestamp=140&end_timestamp=100", customerA.Id),
			fmt.Sprintf("/api/internal/v1/reseller/management/tasks?customer_id=%d&p=0", customerA.Id),
			fmt.Sprintf("/api/internal/v1/reseller/management/tasks?customer_id=%d&page_size=101", customerA.Id),
			fmt.Sprintf("/api/internal/v1/reseller/management/tasks?customer_id=%d&task_id=task-after-120&task_id=task-after-130", customerA.Id),
		} {
			recorder := request(http.MethodGet, path, "", "matrix-reseller-management-test-token", "tasks-invalid_123", adminHeaders)
			envelope := decodeM2Envelope(t, recorder)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Equal(t, middleware.ResellerErrorInvalidRequest, envelope.Error.Code)
		}

		crossTenant := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/reseller/management/tasks?customer_id=%d", customerB.Id), "", "matrix-reseller-management-test-token", "tasks-cross-tenant_123", adminHeaders)
		crossTenantEnvelope := decodeM2Envelope(t, crossTenant)
		require.Equal(t, http.StatusNotFound, crossTenant.Code)
		assert.Equal(t, middleware.ResellerErrorNotFound, crossTenantEnvelope.Error.Code)

		empty := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/reseller/management/tasks?customer_id=%d", customerNoUser.Id), "", "matrix-reseller-management-test-token", "tasks-empty_123", adminHeaders)
		emptyEnvelope := decodeM2Envelope(t, empty)
		require.Equal(t, http.StatusOK, empty.Code)
		var emptyData resellerTasksEnvelopeData
		require.NoError(t, common.Unmarshal(emptyEnvelope.RawData, &emptyData))
		assert.Equal(t, int64(0), emptyData.Total)
		assert.Len(t, emptyData.Items, 0)

		assert.True(t, strings.Contains(empty.Body.String(), `"items":[]`))
	})
}

func seedResellerSettlement(t *testing.T, db *gorm.DB, settlement model.ResellerRequestSettlement) {
	t.Helper()
	require.NoError(t, db.Create(&settlement).Error)
}

func seedTask(t *testing.T, db *gorm.DB, task model.Task) {
	t.Helper()
	require.NoError(t, db.Create(&task).Error)
}
