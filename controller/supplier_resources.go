package controller

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func supplierResourceError(c *gin.Context, err error) {
	var problem *model.SupplierResourceError
	if errors.As(err, &problem) {
		c.JSON(problem.Status, gin.H{"success": false, "code": problem.Code, "message": problem.Message, "field_errors": problem.FieldErrors})
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(404, gin.H{"success": false, "code": "resource_not_found", "message": "resource not found"})
		return
	}
	common.SysError("supplier resource API: " + err.Error())
	c.JSON(500, gin.H{"success": false, "code": "supplier_resource_error", "message": "unable to complete this operation; reload the record before retrying"})
}

func SupplierResourceDetail(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		resource, err := model.ReadSupplierResource(model.DB.WithContext(c.Request.Context()), kind, c.Param("id"))
		if err != nil {
			supplierResourceError(c, err)
			return
		}
		c.Header("ETag", service.SupplierResourceETag(kind, resource))
		common.ApiSuccess(c, resource)
	}
}

func SupplierResourceWrite(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		request := service.SupplierResourceMutation{Kind: kind, ID: c.Param("id"), IfMatch: c.GetHeader("If-Match"), IdempotencyKey: c.GetHeader("Idempotency-Key"), Scope: c.Request.URL.Path, UserID: c.GetInt("id")}
		request.Action = "patch"
		if c.Request.Method == http.MethodDelete {
			request.Action = "delete"
		}
		if c.Request.Method == http.MethodPost {
			request.Action = "create"
		}
		if request.Action != "delete" {
			data, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 2*1024*1024))
			if err != nil {
				supplierResourceError(c, model.SupplierFieldError("", "invalid or oversized request body"))
				return
			}
			request.Patch = data
		}
		if kind == "pool" && request.Action == "create" {
			id, err := strconv.ParseInt(c.Param("id"), 10, 64)
			if err != nil || id <= 0 {
				supplierResourceError(c, model.SupplierFieldError("supplier_id", "invalid supplier ID"))
				return
			}
			request.SupplierID = id
		}
		if strings.HasSuffix(c.FullPath(), "/restore") {
			var fields map[string]json.RawMessage
			var revision int64
			if err := common.Unmarshal(request.Patch, &fields); err != nil || len(fields) != 1 {
				supplierResourceError(c, model.SupplierFieldError("revision", "a source revision is required"))
				return
			}
			if err := common.Unmarshal(fields["revision"], &revision); err != nil || revision <= 0 {
				supplierResourceError(c, model.SupplierFieldError("revision", "invalid source revision"))
				return
			}
			request.Action = "patch"
			request.RestoreRevision = revision
		}
		result, err := service.MutateSupplierResource(c.Request.Context(), request)
		if err != nil {
			supplierResourceError(c, err)
			return
		}
		c.Header("ETag", service.SupplierResourceETag(kind, result.Resource))
		id, version := model.SupplierResourceIdentity(result.Resource)
		if !result.Replayed {
			recordManageAudit(c, "supplier_routing."+kind+"."+request.Action, map[string]interface{}{"resource_id": id, "version": version, "revision": result.Revision, "application": result.Application, "restore_revision": request.RestoreRevision})
		}
		c.JSON(service.SupplierMutationHTTPStatus(result, request.Action), gin.H{"success": true, "message": "", "data": result})
	}
}

func SupplierResourceList(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg, err := model.ReadSupplierResources(model.DB.WithContext(c.Request.Context()))
		if err != nil {
			supplierResourceError(c, err)
			return
		}
		items := []any{}
		q := strings.ToLower(strings.TrimSpace(c.Query("q")))
		owner := c.Query("supplier_id")
		if kind == "pool" {
			owner = c.Param("id")
		}
		poolFilter, channelFilter, modelFilter := c.Query("pool_id"), c.Query("channel_id"), c.Query("model")
		owners := map[int64]int64{}
		supplierModels := map[int64]map[string]bool{}
		for _, p := range cfg.Pools {
			owners[p.ID] = p.SupplierID
			if supplierModels[p.SupplierID] == nil {
				supplierModels[p.SupplierID] = map[string]bool{}
			}
			for _, m := range p.Models {
				supplierModels[p.SupplierID][m.Name] = true
			}
		}
		enabled := c.Query("enabled")
		if enabled != "" && enabled != "true" && enabled != "false" {
			supplierResourceError(c, model.SupplierFieldError("enabled", "use true or false"))
			return
		}
		switch kind {
		case "supplier":
			for _, v := range cfg.Suppliers {
				if q != "" && !strings.Contains(strings.ToLower(v.Name+" "+v.Region), q) {
					continue
				}
				if enabled != "" && v.Enabled != (enabled == "true") {
					continue
				}
				if c.Query("region") != "" && v.Region != c.Query("region") {
					continue
				}
				if modelFilter != "" && !supplierModels[v.ID][modelFilter] {
					continue
				}
				items = append(items, v)
			}
		case "pool":
			for _, v := range cfg.Pools {
				if owner == "" || strconv.FormatInt(v.SupplierID, 10) == owner {
					items = append(items, v)
				}
			}
		case "binding":
			for _, v := range cfg.Bindings {
				if owner != "" && strconv.FormatInt(owners[v.PoolID], 10) != owner {
					continue
				}
				if poolFilter != "" && strconv.FormatInt(v.PoolID, 10) != poolFilter {
					continue
				}
				if channelFilter != "" && strconv.Itoa(v.ChannelID) != channelFilter {
					continue
				}
				if modelFilter != "" && v.Model != modelFilter {
					continue
				}
				items = append(items, v)
			}
		case "rule":
			for _, v := range cfg.Rules {
				if modelFilter == "" || v.Model == modelFilter {
					items = append(items, v)
				}
			}
		}
		page, size := 1, 20
		if c.Query("page") != "" {
			page, err = strconv.Atoi(c.Query("page"))
			if err != nil || page < 1 || page > 1000000 {
				supplierResourceError(c, model.SupplierFieldError("page", "invalid page"))
				return
			}
		}
		if c.Query("page_size") != "" {
			size, err = strconv.Atoi(c.Query("page_size"))
			if err != nil || size < 1 || size > 256 {
				supplierResourceError(c, model.SupplierFieldError("page_size", "page_size must be 1..256"))
				return
			}
		}
		total := len(items)
		start := min((page-1)*size, total)
		end := min(start+size, total)
		common.ApiSuccess(c, gin.H{"items": items[start:end], "total": total, "page": page, "page_size": size})
	}
}

func GetSupplierResourceStatus(c *gin.Context) {
	var state model.SupplierResourceState
	if err := model.ReadSupplierOption(model.DB.WithContext(c.Request.Context()), model.SupplierResourceStateKey, &state); err != nil {
		supplierResourceError(c, err)
		return
	}
	application := "applied"
	if state.TargetRevision != state.AppliedRevision {
		application = "pending"
	}
	// Read both SQL and Redis: a lost ledger or paused gate is not an applied configuration.
	runtime, err := service.ReadSupplierRuntime(c.Request.Context())
	if err != nil || runtime.Config.Revision != state.TargetRevision {
		application = "pending"
	}
	common.ApiSuccess(c, gin.H{"target_revision": state.TargetRevision, "applied_revision": state.AppliedRevision, "application": application})
}
func GetSupplierResourceRevisions(c *gin.Context) {
	var rows []model.RoutingRevision
	query := model.DB.WithContext(c.Request.Context()).Select("id", "created_at", "created_by", "resource_kind", "resource_id", "applied_at").Order("id DESC").Limit(100)
	if kind := c.Query("resource_kind"); kind != "" {
		query = query.Where("resource_kind = ? OR resource_kind = ?", kind, "")
	}
	if id := c.Query("resource_id"); id != "" {
		query = query.Where("resource_id = ? OR resource_kind = ?", id, "")
	}
	if err := query.Find(&rows).Error; err != nil {
		supplierResourceError(c, err)
		return
	}
	common.ApiSuccess(c, rows)
}
func GetSupplierResourceRevision(c *gin.Context) {
	var revision model.RoutingRevision
	if err := model.DB.WithContext(c.Request.Context()).Where("id = ?", c.Param("id")).First(&revision).Error; err != nil {
		supplierResourceError(c, err)
		return
	}
	var cfg model.SupplierRoutingConfig
	if err := common.UnmarshalJsonStr(revision.ConfigJSON, &cfg); err != nil {
		supplierResourceError(c, err)
		return
	}
	revision.ConfigJSON = ""
	common.ApiSuccess(c, gin.H{"revision": revision, "config": cfg})
}
