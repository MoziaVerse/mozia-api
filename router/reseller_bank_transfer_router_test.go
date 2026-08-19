package router

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResellerBankTransferContract(t *testing.T) {
	_, db, request := setupResellerM2Test(t)
	reseller := seedResellerM2(t, db, "Bank Agency", "bank.example.com", model.ResellerRoleOwner, "bank-owner", "bank-admin", "bank-viewer")
	customer := seedCustomerM2(t, db, reseller.Id, "bank-customer", model.ResellerCustomerStatusActive)
	otherReseller := seedResellerM2(t, db, "Other Agency", "other-bank.example.com", model.ResellerRoleOwner, "other-bank-owner", "other-bank-admin", "other-bank-viewer")
	otherCustomer := seedCustomerM2(t, db, otherReseller.Id, "other-bank-customer", model.ResellerCustomerStatusActive)
	headers := func(subject string) map[string]string {
		return map[string]string{"X-Reseller-Subject": subject, "X-Reseller-Host": "bank.example.com"}
	}

	for _, subject := range []string{"bank-owner", "bank-admin", "bank-viewer"} {
		recorder := request(http.MethodPut, "/api/internal/v1/reseller/management/payment/bank-transfer", `{"account_name":"A","account_number":"1","bank_name":"B"}`, "matrix-reseller-management-test-token", "bank-disabled_123", headers(subject))
		response := decodeM2Envelope(t, recorder)
		require.Equal(t, http.StatusForbidden, recorder.Code)
		assert.Equal(t, middleware.ResellerErrorForbidden, response.Error.Code)
	}

	recorder := request(http.MethodPut, fmt.Sprintf("/api/internal/v1/platform/resellers/%d/payment/bank-transfer", reseller.Id), `{"payment_config_enabled":true,"account_name":"","account_number":"","bank_name":""}`, "mozia-mega-test-token", "bank-enable_123", nil)
	require.Equal(t, http.StatusOK, recorder.Code)

	recorder = request(http.MethodPost, "/api/internal/v1/reseller/registration/customers/payment-method", `{"subject":"bank-customer"}`, "matrix-reseller-registration-test-token", "bank-pending_123", nil)
	response := decodeM2Envelope(t, recorder)
	require.Equal(t, http.StatusOK, recorder.Code)
	var pending model.ResellerCustomerPaymentMethod
	require.NoError(t, common.Unmarshal(response.RawData, &pending))
	assert.Equal(t, "bank_transfer", pending.Mode)
	require.NotNil(t, pending.BankTransfer)
	assert.False(t, pending.BankTransfer.Configured)

	recorder = request(http.MethodPut, "/api/internal/v1/reseller/management/payment/bank-transfer", `{"account_name":"杭州量棱文化有限公司","account_number":"57192390001","bank_name":"招商银行杭州支行"}`, "matrix-reseller-management-test-token", "bank-owner_123", headers("bank-owner"))
	require.Equal(t, http.StatusOK, recorder.Code)

	for _, subject := range []string{"bank-admin", "bank-viewer"} {
		recorder = request(http.MethodPut, "/api/internal/v1/reseller/management/payment/bank-transfer", `{"account_name":"X","account_number":"2","bank_name":"Y"}`, "matrix-reseller-management-test-token", "bank-role_123", headers(subject))
		require.Equal(t, http.StatusForbidden, recorder.Code)
	}

	recorder = request(http.MethodPost, "/api/internal/v1/reseller/registration/customers/payment-method", `{"subject":"bank-customer"}`, "matrix-reseller-registration-test-token", "bank-configured_123", nil)
	response = decodeM2Envelope(t, recorder)
	var configured model.ResellerCustomerPaymentMethod
	require.NoError(t, common.Unmarshal(response.RawData, &configured))
	require.Equal(t, "bank_transfer", configured.Mode)
	require.NotNil(t, configured.BankTransfer)
	assert.True(t, configured.BankTransfer.Configured)
	assert.Equal(t, "57192390001", configured.BankTransfer.AccountNumber)

	defaultPreference := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/reseller/management/customers/%d", customer.Id), "", "matrix-reseller-management-test-token", "bank-preference-default_123", headers("bank-admin"))
	response = decodeM2Envelope(t, defaultPreference)
	require.Equal(t, http.StatusOK, defaultPreference.Code)
	var defaultCustomer resellerM2Customer
	require.NoError(t, common.Unmarshal(response.RawData, &defaultCustomer))
	assert.True(t, defaultCustomer.UseResellerPayment)

	viewerPreference := request(http.MethodPatch, fmt.Sprintf("/api/internal/v1/reseller/management/customers/%d/reseller-payment", customer.Id), `{"enabled":false}`, "matrix-reseller-management-test-token", "bank-preference-viewer_123", headers("bank-viewer"))
	require.Equal(t, http.StatusForbidden, viewerPreference.Code)

	forgedPreference := request(http.MethodPatch, fmt.Sprintf("/api/internal/v1/reseller/management/customers/%d/reseller-payment", customer.Id), `{"enabled":false,"reseller_id":999}`, "matrix-reseller-management-test-token", "bank-preference-forged_123", headers("bank-admin"))
	require.Equal(t, http.StatusBadRequest, forgedPreference.Code)

	crossTenantPreference := request(http.MethodPatch, fmt.Sprintf("/api/internal/v1/reseller/management/customers/%d/reseller-payment", otherCustomer.Id), `{"enabled":false}`, "matrix-reseller-management-test-token", "bank-preference-cross-tenant_123", headers("bank-admin"))
	require.Equal(t, http.StatusNotFound, crossTenantPreference.Code)

	disablePreference := request(http.MethodPatch, fmt.Sprintf("/api/internal/v1/reseller/management/customers/%d/reseller-payment", customer.Id), `{"enabled":false}`, "matrix-reseller-management-test-token", "bank-preference-disable_123", headers("bank-admin"))
	response = decodeM2Envelope(t, disablePreference)
	require.Equal(t, http.StatusOK, disablePreference.Code)
	var disabledCustomer resellerM2Customer
	require.NoError(t, common.Unmarshal(response.RawData, &disabledCustomer))
	assert.False(t, disabledCustomer.UseResellerPayment)

	platformPayment := request(http.MethodPost, "/api/internal/v1/reseller/registration/customers/payment-method", `{"subject":"bank-customer"}`, "matrix-reseller-registration-test-token", "bank-preference-platform_123", nil)
	response = decodeM2Envelope(t, platformPayment)
	require.Equal(t, http.StatusOK, platformPayment.Code)
	var customerPlatform model.ResellerCustomerPaymentMethod
	require.NoError(t, common.Unmarshal(response.RawData, &customerPlatform))
	assert.Equal(t, "platform", customerPlatform.Mode)

	enablePreference := request(http.MethodPatch, fmt.Sprintf("/api/internal/v1/reseller/management/customers/%d/reseller-payment", customer.Id), `{"enabled":true}`, "matrix-reseller-management-test-token", "bank-preference-enable_123", headers("bank-owner"))
	require.Equal(t, http.StatusOK, enablePreference.Code)

	resellerPayment := request(http.MethodPost, "/api/internal/v1/reseller/registration/customers/payment-method", `{"subject":"bank-customer"}`, "matrix-reseller-registration-test-token", "bank-preference-reseller_123", nil)
	response = decodeM2Envelope(t, resellerPayment)
	require.Equal(t, http.StatusOK, resellerPayment.Code)
	var customerReseller model.ResellerCustomerPaymentMethod
	require.NoError(t, common.Unmarshal(response.RawData, &customerReseller))
	assert.Equal(t, "bank_transfer", customerReseller.Mode)

	recorder = request(http.MethodPost, "/api/internal/v1/reseller/registration/customers/payment-method", `{"subject":"ordinary-customer"}`, "matrix-reseller-registration-test-token", "bank-platform_123", nil)
	response = decodeM2Envelope(t, recorder)
	var platform model.ResellerCustomerPaymentMethod
	require.NoError(t, common.Unmarshal(response.RawData, &platform))
	assert.Equal(t, "platform", platform.Mode)

	recorder = request(http.MethodPost, "/api/internal/v1/reseller/registration/customers/payment-method", fmt.Sprintf(`{"subject":"bank-customer","reseller_id":%d}`, reseller.Id), "matrix-reseller-registration-test-token", "bank-forged_123", nil)
	response = decodeM2Envelope(t, recorder)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Equal(t, middleware.ResellerErrorInvalidRequest, response.Error.Code)
}
