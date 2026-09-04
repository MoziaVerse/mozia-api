package router

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResellerHostRegistrationContract(t *testing.T) {
	_, db, request := setupResellerM2Test(t)
	const path = "/api/internal/v1/reseller/registration/customers/register"
	const token = "matrix-reseller-registration-test-token"
	reseller := seedResellerM2(t, db, "Agency A", "admin-a.example.com", model.ResellerRoleOwner, "owner-a", "admin-a", "viewer-a")
	require.NoError(t, db.Model(&reseller).Update("matrix_host", "matrix-a.example.com").Error)
	otherHost, disabledHost := "matrix-b.example.com", "disabled.example.com"
	other := model.Reseller{Name: "Agency B", Status: model.ResellerStatusActive, MatrixHost: &otherHost}
	suspended := model.Reseller{Name: "Suspended", Status: model.ResellerStatusSuspended, MatrixHost: &disabledHost}
	require.NoError(t, db.Create(&other).Error)
	require.NoError(t, db.Create(&suspended).Error)

	t.Run("only the registration service can assign and cannot supply a reseller id", func(t *testing.T) {
		for _, unauthorized := range []string{"", "matrix-reseller-management-test-token", "mozia-mega-test-token"} {
			recorder := request(http.MethodPost, path, `{"host":"matrix-a.example.com","subject":"new-customer"}`, unauthorized, "register-auth_123", nil)
			assert.Equal(t, http.StatusUnauthorized, recorder.Code)
		}
		for _, body := range []string{
			`{"host":"matrix-a.example.com","subject":"new-customer","reseller_id":1}`,
			`{"host":"https://matrix-a.example.com/","subject":"new-customer"}`,
			`{"host":"matrix-a.example.com","subject":""}`,
		} {
			recorder := request(http.MethodPost, path, body, token, "register-invalid_123", nil)
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
		}
	})

	t.Run("unbound disabled and management hosts do not assign", func(t *testing.T) {
		for _, host := range []string{"matrix.example.com", "disabled.example.com", "admin-a.example.com"} {
			recorder := request(http.MethodPost, path, fmt.Sprintf(`{"host":%q,"subject":"direct-customer"}`, host), token, "register-direct_123", nil)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			assert.JSONEq(t, "null", string(decodeM2Envelope(t, recorder).RawData))
		}
		var count int64
		require.NoError(t, db.Model(&model.ResellerCustomer{}).Count(&count).Error)
		assert.Zero(t, count)
	})

	t.Run("normalized branded host assigns once without changing payment or later ownership", func(t *testing.T) {
		for _, host := range []string{"MATRIX-A.EXAMPLE.COM.:443", "matrix-a.example.com", "matrix-b.example.com"} {
			recorder := request(http.MethodPost, path, fmt.Sprintf(`{"host":%q,"subject":"new-customer","matrix_name":" Matrix User ","phone":" 13800138000 "}`, host), token, "register-customer_123", nil)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			response := decodeM2Envelope(t, recorder)
			assert.Equal(t, "register-customer_123", response.RequestId)
			var owner model.ResellerRegistrationRecord
			require.NoError(t, common.Unmarshal(response.RawData, &owner))
			assert.Equal(t, reseller.Id, owner.ResellerId)
			assert.Equal(t, reseller.Name, owner.ResellerName)
		}
		var customers []model.ResellerCustomer
		require.NoError(t, db.Find(&customers).Error)
		require.Len(t, customers, 1)
		assert.Equal(t, reseller.Id, customers[0].ResellerId)
		assert.Equal(t, "Matrix User", customers[0].MatrixName)
		assert.Equal(t, "13800138000", customers[0].Phone)
		assert.Equal(t, model.ResellerCustomerStatusActive, customers[0].Status)
		require.NotNil(t, customers[0].UseResellerPayment)
		assert.False(t, *customers[0].UseResellerPayment)
	})

	t.Run("a suspended customer is not reactivated", func(t *testing.T) {
		require.NoError(t, db.Model(&model.ResellerCustomer{}).Where("subject = ?", "new-customer").Update("status", model.ResellerCustomerStatusSuspend).Error)
		recorder := request(http.MethodPost, path, `{"host":"matrix-b.example.com","subject":"new-customer"}`, token, "register-suspended_123", nil)
		require.Equal(t, http.StatusOK, recorder.Code)
		var customer model.ResellerCustomer
		require.NoError(t, db.Where("subject = ?", "new-customer").Take(&customer).Error)
		assert.Equal(t, reseller.Id, customer.ResellerId)
		assert.Equal(t, model.ResellerCustomerStatusSuspend, customer.Status)
	})
}
