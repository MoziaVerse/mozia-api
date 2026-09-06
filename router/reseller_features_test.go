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

func TestResellerFeatureSettingsContract(t *testing.T) {
	_, db, request := setupResellerAdminTest(t)
	t.Setenv("MATRIX_RESELLER_REGISTRATION_TOKEN", "registration-test-token")
	agency := seedReseller(t, db, "Agency", model.ResellerStatusActive, "reseller.example.com", "owner")
	other := seedReseller(t, db, "Other", model.ResellerStatusActive, "other.example.com", "other-owner")
	require.NoError(t, db.Model(&agency).Update("matrix_host", "portal.example.com").Error)
	endpoint := fmt.Sprintf("/api/internal/v1/platform/resellers/%d/presentation", agency.Id)
	legacyBody := `{"brand_name":"Agency","logo":"","favicon":""}`
	defaults, err := model.GetResellerBranding(agency.Id)
	require.NoError(t, err)
	assert.Equal(t, model.ResellerFeatures{}, defaults.Features)

	for _, features := range []model.ResellerFeatures{
		{AppsDisabled: true}, {SkillsDisabled: true}, {VerificationDisabled: true}, {QoderDisabled: true},
		{AppsDisabled: true, SkillsDisabled: true, VerificationDisabled: true, QoderDisabled: true}, {},
	} {
		body, err := common.Marshal(map[string]any{"brand_name": "Agency", "logo": "", "favicon": "", "features": features})
		require.NoError(t, err)
		response := request(http.MethodPut, endpoint, string(body), "mozia-mega-test-token", "features-save_123")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var saved struct{ Data model.ResellerBranding }
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &saved))
		assert.Equal(t, features, saved.Data.Features)

		// Branding-only clients must preserve explicit disabled flags.
		response = request(http.MethodPut, endpoint, legacyBody, "mozia-mega-test-token", "features-old_123")
		require.Equal(t, http.StatusOK, response.Code)
		_, err = model.UpdateResellerLogo(agency.Id, "")
		require.NoError(t, err)
		resolved := request(http.MethodPost, "/api/internal/v1/reseller/registration/presentation", `{"host":"portal.example.com"}`, "registration-test-token", "features-host_123")
		require.Equal(t, http.StatusOK, resolved.Code)
		var presentation struct{ Data model.ResellerPresentation }
		require.NoError(t, common.Unmarshal(resolved.Body.Bytes(), &presentation))
		assert.Equal(t, features, presentation.Data.Features)
		resellerPresentation, err := model.ResolveResellerPresentation("reseller.example.com")
		require.NoError(t, err)
		assert.Equal(t, features, resellerPresentation.Features)
		list := request(http.MethodGet, "/api/internal/v1/platform/resellers", "", "mozia-mega-test-token", "features-list_123")
		require.Equal(t, http.StatusOK, list.Code)
		var records struct{ Data []model.ResellerAdminRecord }
		require.NoError(t, common.Unmarshal(list.Body.Bytes(), &records))
		require.Len(t, records.Data, 2)
		assert.Equal(t, features, records.Data[0].Features)
		unaffected, err := model.GetResellerBranding(other.Id)
		require.NoError(t, err)
		assert.Equal(t, model.ResellerFeatures{}, unaffected.Features)
	}
	invalid := request(http.MethodPut, endpoint, `{"brand_name":"Agency","logo":"","favicon":"","features":{"apps_disabled":"false"}}`, "mozia-mega-test-token", "features-bad_123")
	assert.Equal(t, http.StatusBadRequest, invalid.Code)
	unauthorized := request(http.MethodPut, endpoint, legacyBody, "matrix-reseller-test-token", "features-auth_123")
	assert.Equal(t, http.StatusUnauthorized, unauthorized.Code)
}
