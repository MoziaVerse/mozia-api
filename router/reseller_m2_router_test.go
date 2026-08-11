package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type resellerM2Envelope struct {
	Success bool `json:"success"`
	Error   struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	RequestId string          `json:"request_id"`
	RawData   json.RawMessage `json:"data"`
}

type resellerM2Profile struct {
	ResellerId   int      `json:"reseller_id"`
	ResellerName string   `json:"reseller_name"`
	Host         string   `json:"host"`
	Subject      string   `json:"subject"`
	Role         string   `json:"role"`
	Permissions  []string `json:"permissions"`
}

type resellerM2Customer struct {
	Id                    int     `json:"id"`
	Subject               string  `json:"subject"`
	Status                string  `json:"status"`
	JoinedAt              int64   `json:"joined_at"`
	UserId                int     `json:"user_id"`
	Username              string  `json:"username"`
	DisplayName           string  `json:"display_name"`
	MatrixName            string  `json:"matrix_name"`
	Phone                 string  `json:"phone"`
	ProfileSyncedAt       int64   `json:"profile_synced_at"`
	Remark                *string `json:"remark,omitempty"`
	Balance               float64 `json:"balance"`
	GiftBalance           float64 `json:"gift_balance"`
	PaidBalance           float64 `json:"paid_balance"`
	RequestCount          int     `json:"request_count"`
	BalanceDisplayType    string  `json:"balance_display_type"`
	BalanceCurrencySymbol string  `json:"balance_currency_symbol"`
}

type resellerM2Invitation struct {
	Id                int     `json:"id"`
	CreatedBySubject  string  `json:"created_by_subject"`
	ExpiresAt         int64   `json:"expires_at"`
	RevokedAt         *int64  `json:"revoked_at"`
	ConsumedAt        *int64  `json:"consumed_at"`
	ConsumedBySubject *string `json:"consumed_by_subject"`
	CreatedAt         int64   `json:"created_at"`
	Status            string  `json:"status"`
}

type resellerM2InvitationCreate struct {
	Invitation resellerM2Invitation `json:"invitation"`
	Token      string               `json:"token"`
}

type resellerM2Consume struct {
	Customer     resellerM2Customer `json:"customer"`
	ResellerId   int                `json:"reseller_id"`
	ResellerName string             `json:"reseller_name"`
}

type resellerM2Transfer struct {
	PreviousResellerId int                `json:"previous_reseller_id"`
	TargetResellerId   int                `json:"target_reseller_id"`
	Customer           resellerM2Customer `json:"customer"`
}

type resellerM2BatchAssignResult struct {
	Subject           string `json:"subject"`
	Status            string `json:"status"`
	CustomerId        *int   `json:"customer_id,omitempty"`
	CurrentResellerId *int   `json:"current_reseller_id,omitempty"`
}

type resellerM2BatchAssign struct {
	ResellerId int                           `json:"reseller_id"`
	Results    []resellerM2BatchAssignResult `json:"results"`
}

type resellerM2Request func(method string, path string, body string, token string, requestID string, headers map[string]string) *httptest.ResponseRecorder

func TestResellerM2Contract(t *testing.T) {
	_, db, request := setupResellerM2Test(t)
	resellerA := seedResellerM2(t, db, "Agency A", "portal-a.example.com", model.ResellerRoleOwner, "owner-a", "admin-a", "viewer-a")
	resellerB := seedResellerM2(t, db, "Agency B", "portal-b.example.com", model.ResellerRoleOwner, "owner-b", "admin-b", "viewer-b")

	t.Run("tokens are not interchangeable", func(t *testing.T) {
		management := request(http.MethodGet, "/api/internal/v1/reseller/management/profile", "", "matrix-reseller-test-token", "management-auth_123", map[string]string{
			"X-Reseller-Subject": "owner-a",
			"X-Reseller-Host":    "portal-a.example.com",
		})
		managementResponse := decodeM2Envelope(t, management)
		require.Equal(t, http.StatusUnauthorized, management.Code)
		assert.Equal(t, middleware.ResellerErrorServiceUnauthorized, managementResponse.Error.Code)

		context := request(http.MethodPost, "/api/internal/v1/reseller/context", `{"subject":"owner-a","host":"portal-a.example.com"}`, "matrix-reseller-management-test-token", "context-auth_123", nil)
		contextResponse := decodeM2Envelope(t, context)
		require.Equal(t, http.StatusUnauthorized, context.Code)
		assert.Equal(t, middleware.ResellerErrorServiceUnauthorized, contextResponse.Error.Code)

		registration := request(http.MethodPost, "/api/internal/v1/reseller/registration/invitations/consume", `{"token":"abc","subject":"customer-a"}`, "matrix-reseller-management-test-token", "registration-auth_123", nil)
		registrationResponse := decodeM2Envelope(t, registration)
		require.Equal(t, http.StatusUnauthorized, registration.Code)
		assert.Equal(t, middleware.ResellerErrorServiceUnauthorized, registrationResponse.Error.Code)
	})

	t.Run("empty customer list is an array", func(t *testing.T) {
		recorder := request(http.MethodGet, "/api/internal/v1/reseller/management/customers", "", "matrix-reseller-management-test-token", "empty-customers_123", map[string]string{
			"X-Reseller-Subject": "owner-a",
			"X-Reseller-Host":    "portal-a.example.com",
		})
		response := decodeM2Envelope(t, recorder)
		require.Equal(t, http.StatusOK, recorder.Code)
		assert.JSONEq(t, "[]", string(response.RawData))
	})

	t.Run("viewer cannot mutate and cannot forge reseller id", func(t *testing.T) {
		recorder := request(http.MethodPost, "/api/internal/v1/reseller/management/invitations", `{"expires_in_hours":24,"reseller_id":999}`, "matrix-reseller-management-test-token", "viewer-write_123", map[string]string{
			"X-Reseller-Subject": "viewer-a",
			"X-Reseller-Host":    "portal-a.example.com",
		})
		response := decodeM2Envelope(t, recorder)
		require.Equal(t, http.StatusForbidden, recorder.Code)
		assert.Equal(t, middleware.ResellerErrorForbidden, response.Error.Code)
	})

	t.Run("management profile and cross-tenant customer lookup are scoped by headers", func(t *testing.T) {
		customer := seedCustomerM2(t, db, resellerB.Id, "customer-b1", model.ResellerCustomerStatusActive)

		profileRecorder := request(http.MethodGet, "/api/internal/v1/reseller/management/profile", "", "matrix-reseller-management-test-token", "profile_123", map[string]string{
			"X-Reseller-Subject": "admin-a",
			"X-Reseller-Host":    "PORTAL-A.EXAMPLE.COM.:443",
		})
		profileResponse := decodeM2Envelope(t, profileRecorder)
		var profile resellerM2Profile
		require.NoError(t, common.Unmarshal(profileResponse.RawData, &profile))
		require.Equal(t, http.StatusOK, profileRecorder.Code)
		assert.Equal(t, resellerA.Id, profile.ResellerId)
		assert.Equal(t, "portal-a.example.com", profile.Host)
		assert.Equal(t, "admin-a", profile.Subject)
		assert.Equal(t, model.ResellerRoleAdmin, profile.Role)

		customerRecorder := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/reseller/management/customers/%d", customer.Id), "", "matrix-reseller-management-test-token", "cross-tenant_123", map[string]string{
			"X-Reseller-Subject": "admin-a",
			"X-Reseller-Host":    "portal-a.example.com",
		})
		customerResponse := decodeM2Envelope(t, customerRecorder)
		require.Equal(t, http.StatusNotFound, customerRecorder.Code)
		assert.Equal(t, middleware.ResellerErrorNotFound, customerResponse.Error.Code)
	})

	t.Run("invitation list omits token and revoke changes status", func(t *testing.T) {
		createRecorder := request(http.MethodPost, "/api/internal/v1/reseller/management/invitations", `{"expires_in_hours":12}`, "matrix-reseller-management-test-token", "invite-create_123", map[string]string{
			"X-Reseller-Subject": "owner-a",
			"X-Reseller-Host":    "portal-a.example.com",
		})
		createResponse := decodeM2Envelope(t, createRecorder)
		var created resellerM2InvitationCreate
		require.NoError(t, common.Unmarshal(createResponse.RawData, &created))
		require.Equal(t, http.StatusCreated, createRecorder.Code)
		assert.NotEmpty(t, created.Token)
		assert.Equal(t, model.ResellerInvitationStatusPending, created.Invitation.Status)
		assert.Equal(t, "owner-a", created.Invitation.CreatedBySubject)

		listRecorder := request(http.MethodGet, "/api/internal/v1/reseller/management/invitations", "", "matrix-reseller-management-test-token", "invite-list_123", map[string]string{
			"X-Reseller-Subject": "owner-a",
			"X-Reseller-Host":    "portal-a.example.com",
		})
		listResponse := decodeM2Envelope(t, listRecorder)
		var invitations []map[string]any
		require.NoError(t, common.Unmarshal(listResponse.RawData, &invitations))
		require.Equal(t, http.StatusOK, listRecorder.Code)
		require.Len(t, invitations, 1)
		_, hasToken := invitations[0]["token"]
		assert.False(t, hasToken)

		revokeRecorder := request(http.MethodPost, fmt.Sprintf("/api/internal/v1/reseller/management/invitations/%d/revoke", created.Invitation.Id), "", "matrix-reseller-management-test-token", "invite-revoke_123", map[string]string{
			"X-Reseller-Subject": "owner-a",
			"X-Reseller-Host":    "portal-a.example.com",
		})
		revokeResponse := decodeM2Envelope(t, revokeRecorder)
		var revoked resellerM2Invitation
		require.NoError(t, common.Unmarshal(revokeResponse.RawData, &revoked))
		require.Equal(t, http.StatusOK, revokeRecorder.Code)
		assert.Equal(t, model.ResellerInvitationStatusRevoked, revoked.Status)
		require.NotNil(t, revoked.RevokedAt)
	})

	t.Run("registration handles success replay expired revoked and unique ownership rollback", func(t *testing.T) {
		successCreate := createInvitationM2(t, request, "owner-a", "portal-a.example.com", 24)
		successConsume := request(http.MethodPost, "/api/internal/v1/reseller/registration/invitations/consume", fmt.Sprintf(`{"token":"%s","subject":"customer-a1","matrix_name":"Matrix 客户 A1","phone":"13800138000"}`, successCreate.Token), "matrix-reseller-registration-test-token", "consume-success_123", nil)
		successResponse := decodeM2Envelope(t, successConsume)
		var consumed resellerM2Consume
		require.NoError(t, common.Unmarshal(successResponse.RawData, &consumed))
		require.Equal(t, http.StatusCreated, successConsume.Code)
		assert.Equal(t, "Agency A", consumed.ResellerName)
		assert.Equal(t, resellerA.Id, consumed.ResellerId)
		assert.Equal(t, "customer-a1", consumed.Customer.Subject)
		assert.Equal(t, "Matrix 客户 A1", consumed.Customer.MatrixName)
		assert.Equal(t, "13800138000", consumed.Customer.Phone)

		replayConsume := request(http.MethodPost, "/api/internal/v1/reseller/registration/invitations/consume", fmt.Sprintf(`{"token":"%s","subject":"customer-a1"}`, successCreate.Token), "matrix-reseller-registration-test-token", "consume-replay_123", nil)
		replayResponse := decodeM2Envelope(t, replayConsume)
		require.Equal(t, http.StatusConflict, replayConsume.Code)
		assert.Equal(t, middleware.ResellerErrorInvitationConsumed, replayResponse.Error.Code)

		expiredCreate := createInvitationM2(t, request, "owner-a", "portal-a.example.com", 24)
		require.NoError(t, db.Model(&model.ResellerInvitation{}).Where("id = ?", expiredCreate.Invitation.Id).Update("expires_at", common.GetTimestamp()-1).Error)
		expiredConsume := request(http.MethodPost, "/api/internal/v1/reseller/registration/invitations/consume", fmt.Sprintf(`{"token":"%s","subject":"customer-a2"}`, expiredCreate.Token), "matrix-reseller-registration-test-token", "consume-expired_123", nil)
		expiredResponse := decodeM2Envelope(t, expiredConsume)
		require.Equal(t, http.StatusConflict, expiredConsume.Code)
		assert.Equal(t, middleware.ResellerErrorInvitationExpired, expiredResponse.Error.Code)

		revokedCreate := createInvitationM2(t, request, "owner-a", "portal-a.example.com", 24)
		require.NoError(t, db.Model(&model.ResellerInvitation{}).Where("id = ?", revokedCreate.Invitation.Id).Update("revoked_at", common.GetTimestamp()).Error)
		revokedConsume := request(http.MethodPost, "/api/internal/v1/reseller/registration/invitations/consume", fmt.Sprintf(`{"token":"%s","subject":"customer-a3"}`, revokedCreate.Token), "matrix-reseller-registration-test-token", "consume-revoked_123", nil)
		revokedResponse := decodeM2Envelope(t, revokedConsume)
		require.Equal(t, http.StatusConflict, revokedConsume.Code)
		assert.Equal(t, middleware.ResellerErrorInvitationRevoked, revokedResponse.Error.Code)

		seedCustomerM2(t, db, resellerB.Id, "customer-owned", model.ResellerCustomerStatusActive)
		conflictCreate := createInvitationM2(t, request, "owner-a", "portal-a.example.com", 24)
		conflictConsume := request(http.MethodPost, "/api/internal/v1/reseller/registration/invitations/consume", fmt.Sprintf(`{"token":"%s","subject":"customer-owned"}`, conflictCreate.Token), "matrix-reseller-registration-test-token", "consume-conflict_123", nil)
		conflictResponse := decodeM2Envelope(t, conflictConsume)
		require.Equal(t, http.StatusConflict, conflictConsume.Code)
		assert.Equal(t, middleware.ResellerErrorConflict, conflictResponse.Error.Code)

		var invitation model.ResellerInvitation
		require.NoError(t, db.First(&invitation, conflictCreate.Invitation.Id).Error)
		assert.Nil(t, invitation.ConsumedAt)
	})

	t.Run("customer identity sync updates existing assignments without creating new ones", func(t *testing.T) {
		customer := seedCustomerM2(t, db, resellerA.Id, "customer-sync", model.ResellerCustomerStatusActive)
		recorder := request(http.MethodPost, "/api/internal/v1/reseller/registration/customers/profile", `{"subject":"customer-sync","matrix_name":"同步后的名称","phone":"13900139000"}`, "matrix-reseller-registration-test-token", "profile-sync_123", nil)
		response := decodeM2Envelope(t, recorder)
		require.Equal(t, http.StatusOK, recorder.Code)
		assert.JSONEq(t, `{"updated":true}`, string(response.RawData))

		require.NoError(t, db.First(&customer, customer.Id).Error)
		assert.Equal(t, "同步后的名称", customer.MatrixName)
		assert.Equal(t, "13900139000", customer.Phone)
		assert.Positive(t, customer.ProfileSyncedAt)

		missing := request(http.MethodPost, "/api/internal/v1/reseller/registration/customers/profile", `{"subject":"not-a-customer","matrix_name":"路人","phone":""}`, "matrix-reseller-registration-test-token", "profile-missing_123", nil)
		missingResponse := decodeM2Envelope(t, missing)
		require.Equal(t, http.StatusOK, missing.Code)
		assert.JSONEq(t, `{"updated":false}`, string(missingResponse.RawData))
	})

	t.Run("profile backfill lists only unsynced customer subjects with bounded pagination", func(t *testing.T) {
		require.NoError(t, db.Model(&model.ResellerCustomer{}).Where("id > ?", 0).Update("profile_synced_at", common.GetTimestamp()).Error)
		first := seedCustomerM2(t, db, resellerA.Id, "customer-backfill-a", model.ResellerCustomerStatusActive)
		second := seedCustomerM2(t, db, resellerB.Id, "customer-backfill-b", model.ResellerCustomerStatusActive)
		seeded := seedCustomerM2(t, db, resellerA.Id, "customer-backfill-complete", model.ResellerCustomerStatusActive)
		require.NoError(t, db.Model(&seeded).Update("profile_synced_at", common.GetTimestamp()).Error)

		unauthorized := request(http.MethodGet, "/api/internal/v1/reseller/registration/customers/pending-profiles", "", "", "backfill-unauthorized_123", nil)
		require.Equal(t, http.StatusUnauthorized, unauthorized.Code)

		pageOne := request(http.MethodGet, "/api/internal/v1/reseller/registration/customers/pending-profiles?limit=1", "", "matrix-reseller-registration-test-token", "backfill-page-one_123", nil)
		pageOneResponse := decodeM2Envelope(t, pageOne)
		require.Equal(t, http.StatusOK, pageOne.Code)
		var firstPage model.ResellerCustomerProfileBackfillPage
		require.NoError(t, common.Unmarshal(pageOneResponse.RawData, &firstPage))
		require.Len(t, firstPage.Items, 1)
		assert.Equal(t, first.Id, firstPage.Items[0].Id)
		assert.Equal(t, "customer-backfill-a", firstPage.Items[0].Subject)
		assert.Equal(t, first.Id, firstPage.NextAfterId)

		pageTwo := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/reseller/registration/customers/pending-profiles?after_id=%d&limit=1", firstPage.NextAfterId), "", "matrix-reseller-registration-test-token", "backfill-page-two_123", nil)
		pageTwoResponse := decodeM2Envelope(t, pageTwo)
		require.Equal(t, http.StatusOK, pageTwo.Code)
		var secondPage model.ResellerCustomerProfileBackfillPage
		require.NoError(t, common.Unmarshal(pageTwoResponse.RawData, &secondPage))
		require.Len(t, secondPage.Items, 1)
		assert.Equal(t, second.Id, secondPage.Items[0].Id)
		assert.Zero(t, secondPage.NextAfterId)

		invalid := request(http.MethodGet, "/api/internal/v1/reseller/registration/customers/pending-profiles?limit=201", "", "matrix-reseller-registration-test-token", "backfill-invalid_123", nil)
		require.Equal(t, http.StatusBadRequest, invalid.Code)
	})

	t.Run("only the reseller owner can read and edit customer remarks", func(t *testing.T) {
		customer := model.ResellerCustomer{
			ResellerId: resellerA.Id,
			Subject:    "customer-remark",
			MatrixName: "备注客户",
			Phone:      "13700137000",
			Remark:     "老板认识的客户",
			Status:     model.ResellerCustomerStatusActive,
		}
		require.NoError(t, db.Create(&customer).Error)

		adminList := request(http.MethodGet, "/api/internal/v1/reseller/management/customers", "", "matrix-reseller-management-test-token", "remark-admin-list_123", map[string]string{
			"X-Reseller-Subject": "admin-a",
			"X-Reseller-Host":    "portal-a.example.com",
		})
		adminResponse := decodeM2Envelope(t, adminList)
		require.Equal(t, http.StatusOK, adminList.Code)
		var adminCustomers []map[string]any
		require.NoError(t, common.Unmarshal(adminResponse.RawData, &adminCustomers))
		found := false
		for _, item := range adminCustomers {
			if int(item["id"].(float64)) == customer.Id {
				found = true
				assert.Equal(t, "备注客户", item["matrix_name"])
				assert.Equal(t, "13700137000", item["phone"])
				_, hasRemark := item["remark"]
				assert.False(t, hasRemark)
			}
		}
		assert.True(t, found)

		adminUpdate := request(http.MethodPatch, fmt.Sprintf("/api/internal/v1/reseller/management/customers/%d/remark", customer.Id), `{"remark":"越权备注"}`, "matrix-reseller-management-test-token", "remark-admin-update_123", map[string]string{
			"X-Reseller-Subject": "admin-a",
			"X-Reseller-Host":    "portal-a.example.com",
		})
		adminUpdateResponse := decodeM2Envelope(t, adminUpdate)
		require.Equal(t, http.StatusForbidden, adminUpdate.Code)
		assert.Equal(t, middleware.ResellerErrorForbidden, adminUpdateResponse.Error.Code)

		crossTenantUpdate := request(http.MethodPatch, fmt.Sprintf("/api/internal/v1/reseller/management/customers/%d/remark", customer.Id), `{"remark":"其他代理商备注"}`, "matrix-reseller-management-test-token", "remark-cross-tenant_123", map[string]string{
			"X-Reseller-Subject": "owner-b",
			"X-Reseller-Host":    "portal-b.example.com",
		})
		crossTenantResponse := decodeM2Envelope(t, crossTenantUpdate)
		require.Equal(t, http.StatusNotFound, crossTenantUpdate.Code)
		assert.Equal(t, middleware.ResellerErrorNotFound, crossTenantResponse.Error.Code)

		ownerUpdate := request(http.MethodPatch, fmt.Sprintf("/api/internal/v1/reseller/management/customers/%d/remark", customer.Id), `{"remark":"  重点客户  "}`, "matrix-reseller-management-test-token", "remark-owner-update_123", map[string]string{
			"X-Reseller-Subject": "owner-a",
			"X-Reseller-Host":    "portal-a.example.com",
		})
		ownerUpdateResponse := decodeM2Envelope(t, ownerUpdate)
		var updated resellerM2Customer
		require.NoError(t, common.Unmarshal(ownerUpdateResponse.RawData, &updated))
		require.Equal(t, http.StatusOK, ownerUpdate.Code)
		require.NotNil(t, updated.Remark)
		assert.Equal(t, "重点客户", *updated.Remark)

		ownerList := request(http.MethodGet, "/api/internal/v1/reseller/management/customers", "", "matrix-reseller-management-test-token", "remark-owner-list_123", map[string]string{
			"X-Reseller-Subject": "owner-a",
			"X-Reseller-Host":    "portal-a.example.com",
		})
		ownerResponse := decodeM2Envelope(t, ownerList)
		require.Equal(t, http.StatusOK, ownerList.Code)
		assert.Contains(t, string(ownerResponse.RawData), `"remark":"重点客户"`)

		platformList := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/platform/resellers/%d/customers", resellerA.Id), "", "mozia-mega-test-token", "remark-platform-list_123", nil)
		platformResponse := decodeM2Envelope(t, platformList)
		require.Equal(t, http.StatusOK, platformList.Code)
		assert.NotContains(t, string(platformResponse.RawData), `"remark"`)
	})

	t.Run("concurrent consume only creates one customer", func(t *testing.T) {
		created := createInvitationM2(t, request, "owner-a", "portal-a.example.com", 24)
		var wg sync.WaitGroup
		statuses := make([]int, 2)
		codes := make([]string, 2)
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				recorder := request(http.MethodPost, "/api/internal/v1/reseller/registration/invitations/consume", fmt.Sprintf(`{"token":"%s","subject":"customer-concurrent"}`, created.Token), "matrix-reseller-registration-test-token", fmt.Sprintf("consume-concurrent_%d", index), nil)
				response := decodeM2Envelope(t, recorder)
				statuses[index] = recorder.Code
				codes[index] = response.Error.Code
			}(i)
		}
		wg.Wait()

		assert.Contains(t, statuses, http.StatusCreated)
		assert.Contains(t, statuses, http.StatusConflict)
		assert.Contains(t, codes, middleware.ResellerErrorInvitationConsumed)

		var customerCount int64
		require.NoError(t, db.Model(&model.ResellerCustomer{}).Where("subject = ?", "customer-concurrent").Count(&customerCount).Error)
		assert.Equal(t, int64(1), customerCount)
	})

	t.Run("customer status update and platform transfer work end to end", func(t *testing.T) {
		customer := seedCustomerM2(t, db, resellerA.Id, "customer-transfer", model.ResellerCustomerStatusActive)
		gatewayUser := model.User{
			Username: "customer-transfer", DisplayName: "客户 Transfer", Password: "test-password", AffCode: "aff-customer-transfer", Quota: 5000000, UsedQuota: 2500000, RequestCount: 17,
		}
		require.NoError(t, db.Create(&gatewayUser).Error)
		require.NoError(t, db.Create(&model.UserSSO{SSOSub: customer.Subject, UserId: gatewayUser.Id}).Error)
		require.NoError(t, db.Create(&[]model.MoziaWalletBalance{
			{UserId: gatewayUser.Id, Source: model.MoziaWalletSourceGift, Balance: 1000000},
			{UserId: gatewayUser.Id, Source: model.MoziaWalletSourcePaid, Balance: 3000000},
		}).Error)

		statusRecorder := request(http.MethodPatch, fmt.Sprintf("/api/internal/v1/reseller/management/customers/%d/status", customer.Id), `{"status":"suspended"}`, "matrix-reseller-management-test-token", "customer-status_123", map[string]string{
			"X-Reseller-Subject": "admin-a",
			"X-Reseller-Host":    "portal-a.example.com",
		})
		statusResponse := decodeM2Envelope(t, statusRecorder)
		var updated resellerM2Customer
		require.NoError(t, common.Unmarshal(statusResponse.RawData, &updated))
		require.Equal(t, http.StatusOK, statusRecorder.Code)
		assert.Equal(t, model.ResellerCustomerStatusSuspend, updated.Status)

		listRecorder := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/platform/resellers/%d/customers", resellerA.Id), "", "mozia-mega-test-token", "platform-list_123", nil)
		listResponse := decodeM2Envelope(t, listRecorder)
		var listed []resellerM2Customer
		require.NoError(t, common.Unmarshal(listResponse.RawData, &listed))
		require.Equal(t, http.StatusOK, listRecorder.Code)
		found := false
		for _, item := range listed {
			if item.Id == customer.Id {
				found = true
				assert.Equal(t, model.ResellerCustomerStatusSuspend, item.Status)
				assert.Equal(t, gatewayUser.Id, item.UserId)
				assert.Equal(t, "customer-transfer", item.Username)
				assert.Equal(t, "客户 Transfer", item.DisplayName)
				assert.Equal(t, 10.0, item.Balance)
				assert.Equal(t, 2.0, item.GiftBalance)
				assert.Equal(t, 6.0, item.PaidBalance)
				assert.Equal(t, operation_setting.QuotaDisplayTypeUSD, item.BalanceDisplayType)
				assert.Equal(t, "$", item.BalanceCurrencySymbol)
				assert.Equal(t, 17, item.RequestCount)
			}
		}
		assert.True(t, found)

		transferRecorder := request(http.MethodPost, fmt.Sprintf("/api/internal/v1/platform/reseller-customers/%d/transfer", customer.Id), fmt.Sprintf(`{"target_reseller_id":%d}`, resellerB.Id), "mozia-mega-test-token", "platform-transfer_123", nil)
		transferResponse := decodeM2Envelope(t, transferRecorder)
		var transferred resellerM2Transfer
		require.NoError(t, common.Unmarshal(transferResponse.RawData, &transferred))
		require.Equal(t, http.StatusOK, transferRecorder.Code)
		assert.Equal(t, resellerA.Id, transferred.PreviousResellerId)
		assert.Equal(t, resellerB.Id, transferred.TargetResellerId)
		assert.Equal(t, model.ResellerCustomerStatusSuspend, transferred.Customer.Status)

		duplicateRecorder := request(http.MethodPost, fmt.Sprintf("/api/internal/v1/platform/reseller-customers/%d/transfer", customer.Id), fmt.Sprintf(`{"target_reseller_id":%d}`, resellerB.Id), "mozia-mega-test-token", "platform-duplicate_123", nil)
		duplicateResponse := decodeM2Envelope(t, duplicateRecorder)
		require.Equal(t, http.StatusConflict, duplicateRecorder.Code)
		assert.Equal(t, middleware.ResellerErrorConflict, duplicateResponse.Error.Code)

		customerRecorder := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/reseller/management/customers/%d", customer.Id), "", "matrix-reseller-management-test-token", "platform-transfer-scope_123", map[string]string{
			"X-Reseller-Subject": "admin-b",
			"X-Reseller-Host":    "portal-b.example.com",
		})
		customerResponse := decodeM2Envelope(t, customerRecorder)
		var moved resellerM2Customer
		require.NoError(t, common.Unmarshal(customerResponse.RawData, &moved))
		require.Equal(t, http.StatusOK, customerRecorder.Code)
		assert.Equal(t, "customer-transfer", moved.Subject)
	})

	t.Run("platform batch assign classifies mixed ownership and retries safely", func(t *testing.T) {
		existingTarget := seedCustomerM2(t, db, resellerA.Id, "customer-batch-target", model.ResellerCustomerStatusActive)
		existingOther := seedCustomerM2(t, db, resellerB.Id, "customer-batch-other", model.ResellerCustomerStatusActive)
		body := `{"customers":[{"subject":"customer-batch-new","matrix_name":"批量新客户","phone":"13600136000"},{"subject":"customer-batch-target","matrix_name":"目标代理商已有客户","phone":"13500135000"},{"subject":"customer-batch-other","matrix_name":"其他代理商客户","phone":"13400134000"}]}`

		recorder := request(http.MethodPost, fmt.Sprintf("/api/internal/v1/platform/resellers/%d/customers/batch-assign", resellerA.Id), body, "mozia-mega-test-token", "platform-batch-assign_123", nil)
		response := decodeM2Envelope(t, recorder)
		var assigned resellerM2BatchAssign
		require.NoError(t, common.Unmarshal(response.RawData, &assigned))
		require.Equal(t, http.StatusOK, recorder.Code)
		require.Len(t, assigned.Results, 3)
		assert.Equal(t, resellerA.Id, assigned.ResellerId)

		first := assigned.Results[0]
		require.NotNil(t, first.CustomerId)
		require.NotNil(t, first.CurrentResellerId)
		assert.Equal(t, "customer-batch-new", first.Subject)
		assert.Equal(t, model.ResellerCustomerBatchAssignStatusAssigned, first.Status)
		assert.Equal(t, resellerA.Id, *first.CurrentResellerId)

		var created model.ResellerCustomer
		require.NoError(t, db.First(&created, *first.CustomerId).Error)
		assert.Equal(t, resellerA.Id, created.ResellerId)
		assert.Equal(t, "批量新客户", created.MatrixName)
		assert.Equal(t, "13600136000", created.Phone)
		assert.Equal(t, model.ResellerCustomerStatusActive, created.Status)
		assert.Positive(t, created.ProfileSyncedAt)

		second := assigned.Results[1]
		require.NotNil(t, second.CustomerId)
		require.NotNil(t, second.CurrentResellerId)
		assert.Equal(t, "customer-batch-target", second.Subject)
		assert.Equal(t, model.ResellerCustomerBatchAssignStatusAlreadyInTarget, second.Status)
		assert.Equal(t, existingTarget.Id, *second.CustomerId)
		assert.Equal(t, resellerA.Id, *second.CurrentResellerId)

		third := assigned.Results[2]
		require.NotNil(t, third.CustomerId)
		require.NotNil(t, third.CurrentResellerId)
		assert.Equal(t, "customer-batch-other", third.Subject)
		assert.Equal(t, model.ResellerCustomerBatchAssignStatusOwnedByOtherReseller, third.Status)
		assert.Equal(t, existingOther.Id, *third.CustomerId)
		assert.Equal(t, resellerB.Id, *third.CurrentResellerId)

		retryRecorder := request(http.MethodPost, fmt.Sprintf("/api/internal/v1/platform/resellers/%d/customers/batch-assign", resellerA.Id), body, "mozia-mega-test-token", "platform-batch-retry_123", nil)
		retryResponse := decodeM2Envelope(t, retryRecorder)
		var retried resellerM2BatchAssign
		require.NoError(t, common.Unmarshal(retryResponse.RawData, &retried))
		require.Equal(t, http.StatusOK, retryRecorder.Code)
		require.Len(t, retried.Results, 3)
		assert.Equal(t, model.ResellerCustomerBatchAssignStatusAlreadyInTarget, retried.Results[0].Status)
		require.NotNil(t, retried.Results[0].CustomerId)
		assert.Equal(t, *first.CustomerId, *retried.Results[0].CustomerId)
		assert.Equal(t, model.ResellerCustomerBatchAssignStatusAlreadyInTarget, retried.Results[1].Status)
		assert.Equal(t, model.ResellerCustomerBatchAssignStatusOwnedByOtherReseller, retried.Results[2].Status)

		var count int64
		require.NoError(t, db.Model(&model.ResellerCustomer{}).Where("subject = ?", "customer-batch-new").Count(&count).Error)
		assert.Equal(t, int64(1), count)
	})

	t.Run("platform batch assign concurrent same subject returns assigned then already_in_target", func(t *testing.T) {
		path := fmt.Sprintf("/api/internal/v1/platform/resellers/%d/customers/batch-assign", resellerA.Id)
		body := `{"customers":[{"subject":"customer-batch-concurrent","matrix_name":"并发客户","phone":"13300133000"}]}`

		type concurrentResult struct {
			code   int
			status string
			err    error
		}
		results := make([]concurrentResult, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				recorder := request(http.MethodPost, path, body, "mozia-mega-test-token", fmt.Sprintf("platform-batch-concurrent_%d", index), nil)
				response := decodeM2Envelope(t, recorder)
				results[index].code = recorder.Code
				if recorder.Code == http.StatusOK {
					var payload resellerM2BatchAssign
					if err := common.Unmarshal(response.RawData, &payload); err != nil {
						results[index].err = err
						return
					}
					if len(payload.Results) != 1 {
						results[index].err = fmt.Errorf("expected 1 result, got %d", len(payload.Results))
						return
					}
					results[index].status = payload.Results[0].Status
				}
			}(i)
		}
		wg.Wait()

		require.NoError(t, results[0].err)
		require.NoError(t, results[1].err)
		assert.Equal(t, http.StatusOK, results[0].code)
		assert.Equal(t, http.StatusOK, results[1].code)
		assert.Contains(t, []string{results[0].status, results[1].status}, model.ResellerCustomerBatchAssignStatusAssigned)
		assert.Contains(t, []string{results[0].status, results[1].status}, model.ResellerCustomerBatchAssignStatusAlreadyInTarget)

		var count int64
		require.NoError(t, db.Model(&model.ResellerCustomer{}).Where("subject = ?", "customer-batch-concurrent").Count(&count).Error)
		assert.Equal(t, int64(1), count)
	})

	t.Run("platform batch assign rejects invalid payloads", func(t *testing.T) {
		cases := []struct {
			name string
			body string
		}{
			{name: "empty", body: `{"customers":[]}`},
			{name: "duplicate subjects", body: `{"customers":[{"subject":"customer-batch-dup"},{"subject":"customer-batch-dup"}]}`},
			{name: "forged reseller scope", body: `{"reseller_id":999,"customers":[{"subject":"customer-batch-scope"}]}`},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				recorder := request(http.MethodPost, fmt.Sprintf("/api/internal/v1/platform/resellers/%d/customers/batch-assign", resellerA.Id), tc.body, "mozia-mega-test-token", "platform-batch-invalid_123", nil)
				response := decodeM2Envelope(t, recorder)
				require.Equal(t, http.StatusBadRequest, recorder.Code)
				assert.Equal(t, middleware.ResellerErrorInvalidRequest, response.Error.Code)
			})
		}
	})

	t.Run("platform batch assign validates all inputs before any write", func(t *testing.T) {
		path := fmt.Sprintf("/api/internal/v1/platform/resellers/%d/customers/batch-assign", resellerA.Id)
		body := `{"customers":[{"subject":"customer-batch-prevalidate","matrix_name":"先合法","phone":"13200132000"},{"subject":" customer-batch-invalid","matrix_name":"后续非法","phone":"13100131000"}]}`

		recorder := request(http.MethodPost, path, body, "mozia-mega-test-token", "platform-batch-prevalidate_123", nil)
		response := decodeM2Envelope(t, recorder)
		require.Equal(t, http.StatusBadRequest, recorder.Code)
		assert.Equal(t, middleware.ResellerErrorInvalidRequest, response.Error.Code)

		var count int64
		require.NoError(t, db.Model(&model.ResellerCustomer{}).Where("subject = ?", "customer-batch-prevalidate").Count(&count).Error)
		assert.Equal(t, int64(0), count)
	})

	t.Run("platform batch assign requires an active target reseller", func(t *testing.T) {
		suspended := seedResellerM2(t, db, "Agency Suspended", "portal-suspended.example.com", model.ResellerRoleOwner, "owner-suspended", "admin-suspended", "viewer-suspended")
		require.NoError(t, db.Model(&model.Reseller{}).Where("id = ?", suspended.Id).Update("status", model.ResellerStatusSuspended).Error)

		cases := []struct {
			name string
			path string
		}{
			{name: "suspended", path: fmt.Sprintf("/api/internal/v1/platform/resellers/%d/customers/batch-assign", suspended.Id)},
			{name: "missing", path: "/api/internal/v1/platform/resellers/999999/customers/batch-assign"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				recorder := request(http.MethodPost, tc.path, `{"customers":[{"subject":"customer-batch-missing"}]}`, "mozia-mega-test-token", "platform-batch-missing_123", nil)
				response := decodeM2Envelope(t, recorder)
				require.Equal(t, http.StatusNotFound, recorder.Code)
				assert.Equal(t, middleware.ResellerErrorNotFound, response.Error.Code)
			})
		}
	})

	t.Run("platform batch assign enforces platform auth", func(t *testing.T) {
		recorder := request(http.MethodPost, fmt.Sprintf("/api/internal/v1/platform/resellers/%d/customers/batch-assign", resellerA.Id), `{"customers":[{"subject":"customer-batch-auth"}]}`, "matrix-reseller-management-test-token", "platform-batch-auth_123", nil)
		response := decodeM2Envelope(t, recorder)
		require.Equal(t, http.StatusUnauthorized, recorder.Code)
		assert.Equal(t, middleware.ResellerErrorServiceUnauthorized, response.Error.Code)
	})
}

func setupResellerM2Test(t *testing.T) (*gin.Engine, *gorm.DB, resellerM2Request) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	originalDB := model.DB
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDisplayType := operation_setting.GetQuotaDisplayType()
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_busy_timeout=30000", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&model.User{}, &model.UserSSO{}, &model.MoziaWalletBalance{}, &model.Reseller{}, &model.ResellerDomain{},
		&model.ResellerMember{}, &model.ResellerCustomer{}, &model.ResellerInvitation{},
		&model.ResellerPriceRule{}, &model.ResellerRequestSettlement{},
	))
	model.DB = db
	t.Setenv("MATRIX_RESELLER_SERVICE_TOKEN", "matrix-reseller-test-token")
	t.Setenv("MATRIX_RESELLER_MANAGEMENT_TOKEN", "matrix-reseller-management-test-token")
	t.Setenv("MATRIX_RESELLER_REGISTRATION_TOKEN", "matrix-reseller-registration-test-token")
	t.Setenv("MOZIA_MEGA_SERVICE_TOKEN", "mozia-mega-test-token")
	t.Cleanup(func() {
		model.DB = originalDB
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	engine := gin.New()
	registerResellerRoutes(engine.Group("/api"))

	request := func(method string, path string, body string, token string, requestID string, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		httpRequest := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			httpRequest.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			httpRequest.Header.Set("Authorization", "Bearer "+token)
		}
		if requestID != "" {
			httpRequest.Header.Set(common.RequestIdKey, requestID)
		}
		for key, value := range headers {
			httpRequest.Header.Set(key, value)
		}
		engine.ServeHTTP(recorder, httpRequest)
		return recorder
	}
	return engine, db, request
}

func seedResellerM2(t *testing.T, db *gorm.DB, name string, host string, ownerRole string, ownerSubject string, adminSubject string, viewerSubject string) model.Reseller {
	t.Helper()
	reseller := model.Reseller{Name: name, Status: model.ResellerStatusActive}
	require.NoError(t, db.Create(&reseller).Error)
	require.NoError(t, db.Create(&model.ResellerDomain{
		ResellerId: reseller.Id,
		Host:       host,
		Verified:   true,
		Status:     model.ResellerDomainStatusActive,
	}).Error)
	require.NoError(t, db.Create(&[]model.ResellerMember{
		{ResellerId: reseller.Id, Subject: ownerSubject, Role: ownerRole, Status: model.ResellerMemberStatusActive},
		{ResellerId: reseller.Id, Subject: adminSubject, Role: model.ResellerRoleAdmin, Status: model.ResellerMemberStatusActive},
		{ResellerId: reseller.Id, Subject: viewerSubject, Role: model.ResellerRoleViewer, Status: model.ResellerMemberStatusActive},
	}).Error)
	return reseller
}

func seedCustomerM2(t *testing.T, db *gorm.DB, resellerId int, subject string, status string) model.ResellerCustomer {
	t.Helper()
	customer := model.ResellerCustomer{ResellerId: resellerId, Subject: subject, Status: status}
	require.NoError(t, db.Create(&customer).Error)
	return customer
}

func createInvitationM2(t *testing.T, request resellerM2Request, subject string, host string, hours int) resellerM2InvitationCreate {
	t.Helper()
	recorder := request(http.MethodPost, "/api/internal/v1/reseller/management/invitations", fmt.Sprintf(`{"expires_in_hours":%d}`, hours), "matrix-reseller-management-test-token", "create-invitation_123", map[string]string{
		"X-Reseller-Subject": subject,
		"X-Reseller-Host":    host,
	})
	response := decodeM2Envelope(t, recorder)
	require.Equal(t, http.StatusCreated, recorder.Code)
	var created resellerM2InvitationCreate
	require.NoError(t, common.Unmarshal(response.RawData, &created))
	return created
}

func decodeM2Envelope(t *testing.T, recorder *httptest.ResponseRecorder) resellerM2Envelope {
	t.Helper()
	var response resellerM2Envelope
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	return response
}
