package controller

import (
	"errors"
	"net/http"
	"sort"
	"strconv"

	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func UpdateResellerAdminModelAccess(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid reseller id")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 300<<10)
	var request struct {
		Restricted *bool     `json:"restricted"`
		Models     *[]string `json:"models"`
	}
	if c.ShouldBindJSON(&request) != nil || request.Restricted == nil || request.Models == nil {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "restricted and models are required")
		return
	}
	policy, err := model.UpdateResellerModelAccess(id, model.ResellerModelAccess{Restricted: *request.Restricted, Models: *request.Models})
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, policy)
	case errors.Is(err, model.ErrInvalidResellerModelAccess):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid or unknown model selection")
	case errors.Is(err, gorm.ErrRecordNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller not found")
	default:
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "unable to save model access")
	}
}

func GetResellerAdminModelCatalog(c *gin.Context) {
	var names []string
	if err := model.DB.Model(&model.Ability{}).Where("enabled = ?", true).Distinct("model").Pluck("model", &names).Error; err != nil {
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "unable to load model catalog")
		return
	}
	sort.Strings(names)
	if names == nil {
		names = []string{}
	}
	writeResellerAdminSuccess(c, http.StatusOK, names)
}
