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

func TestResellerDocumentationPolicyContract(t *testing.T) {
	_, db, request := setupResellerAdminTest(t)
	t.Setenv("MATRIX_RESELLER_REGISTRATION_TOKEN", "registration-test-token")
	agency := seedReseller(t, db, "Agency", model.ResellerStatusActive, "reseller.example.com", "owner")
	other := seedReseller(t, db, "Other", model.ResellerStatusActive, "other.example.com", "other-owner")
	require.NoError(t, db.Model(&agency).Update("matrix_host", "portal.example.com").Error)
	endpoint := fmt.Sprintf("/api/internal/v1/platform/resellers/%d/presentation", agency.Id)
	const destination = "https://help.example.com/start?lang=zh#intro"
	legacyBody := `{"brand_name":"Agency","logo":"","favicon":""}`
	unauthorized := request(http.MethodPut, endpoint, legacyBody, "matrix-reseller-test-token", "docs-auth_123")
	require.Equal(t, http.StatusUnauthorized, unauthorized.Code)

	for _, mode := range []string{"custom", "hidden", "default", "custom"} {
		body := fmt.Sprintf(`{"brand_name":"Agency","logo":"","favicon":"","documentation_mode":%q,"documentation_url":%q}`, mode, destination)
		response := request(http.MethodPut, endpoint, body, "mozia-mega-test-token", "docs-save_123")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		wantURL := ""
		if mode == "custom" {
			wantURL = destination
		}
		var saved struct {
			Data model.ResellerBranding `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &saved))
		assert.Equal(t, mode, saved.Data.DocumentationMode)
		assert.Equal(t, wantURL, saved.Data.DocumentationURL)
		resolved := request(http.MethodPost, "/api/internal/v1/reseller/registration/presentation", `{"host":"portal.example.com"}`, "registration-test-token", "docs-resolve_123")
		require.Equal(t, http.StatusOK, resolved.Code, resolved.Body.String())
		var presentation struct {
			Data model.ResellerPresentation `json:"data"`
		}
		require.NoError(t, common.Unmarshal(resolved.Body.Bytes(), &presentation))
		assert.Equal(t, mode, presentation.Data.DocumentationMode)
		assert.Equal(t, wantURL, presentation.Data.DocumentationURL)
		list := request(http.MethodGet, "/api/internal/v1/platform/resellers", "", "mozia-mega-test-token", "docs-list_123")
		require.Equal(t, http.StatusOK, list.Code)
		var records struct {
			Data []model.ResellerAdminRecord `json:"data"`
		}
		require.NoError(t, common.Unmarshal(list.Body.Bytes(), &records))
		require.Len(t, records.Data, 2)
		assert.Equal(t, mode, records.Data[0].DocumentationMode)
		assert.Equal(t, wantURL, records.Data[0].DocumentationURL)
	}

	for _, fields := range []string{
		`"documentation_mode":"unknown","documentation_url":""`,
		`"documentation_mode":"custom","documentation_url":""`,
		`"documentation_mode":"custom","documentation_url":"javascript:alert(1)"`,
		`"documentation_mode":"custom","documentation_url":"http://docs.example.com"`,
		`"documentation_mode":"custom","documentation_url":"//docs.example.com"`,
		`"documentation_mode":"custom","documentation_url":"https://user:password@docs.example.com"`,
		`"documentation_mode":"custom","documentation_url":"https://docs.example.com/foo bar"`,
		`"documentation_mode":"custom","documentation_url":"https://docs.example.com/\nfoo"`,
		`"documentation_mode":"hidden"`,
		`"documentation_url":"https://docs.example.com"`,
	} {
		body := `{"brand_name":"Must not save","logo":"","favicon":"",` + fields + `}`
		response := request(http.MethodPut, endpoint, body, "mozia-mega-test-token", "docs-invalid_123")
		require.Equal(t, http.StatusBadRequest, response.Code, fields)
	}
	saved, err := model.GetResellerBranding(agency.Id)
	require.NoError(t, err)
	assert.Equal(t, "Agency", saved.BrandName)
	assert.Equal(t, destination, saved.DocumentationURL)

	// Old Mega clients and independent logo updates must not erase the new policy.
	response := request(http.MethodPut, endpoint, legacyBody, "mozia-mega-test-token", "docs-legacy_123")
	require.Equal(t, http.StatusOK, response.Code)
	_, err = model.UpdateResellerLogo(agency.Id, "")
	require.NoError(t, err)
	saved, err = model.GetResellerBranding(agency.Id)
	require.NoError(t, err)
	assert.Equal(t, "custom", saved.DocumentationMode)
	assert.Equal(t, destination, saved.DocumentationURL)
	unaffected, err := model.GetResellerBranding(other.Id)
	require.NoError(t, err)
	assert.Equal(t, "default", unaffected.DocumentationMode)
	assert.Empty(t, unaffected.DocumentationURL)
}
