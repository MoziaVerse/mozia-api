package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestCallAnalyticsRejectsInvalidQueryBeforeDatabaseAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/analytics", GetCallAnalytics)
	router.GET("/analytics/requests", GetCallAnalyticsRequestPage)
	for _, query := range []string{"user_id=not-a-number", "start_timestamp=10&end_timestamp=10", "page_size=101", "outcome=invalid", "channel=-1", "p=-1"} {
		t.Run(query, func(t *testing.T) {
			for _, path := range []string{"/analytics", "/analytics/requests"} {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path+"?"+query, nil))
				assert.Equal(t, http.StatusBadRequest, response.Code)
				assert.Contains(t, response.Body.String(), `"success":false`)
			}
		})
	}
}
