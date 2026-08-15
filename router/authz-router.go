package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

// registerAuthzRoutes mounts the authorization API under its own /authz
// namespace. GET /authz/catalog returns the permission schema (resources,
// actions, and role baselines) used by the client permission editor.
func registerAuthzRoutes(apiRouter *gin.RouterGroup) {
	authzRoute := apiRouter.Group("/authz")
	{
		authzRoute.GET("/catalog", middleware.AdminAuth(), controller.GetPermissionCatalog)
		authzRoute.GET("/admins", middleware.RootAuth(), controller.GetManagedAdmins)
		authzRoute.PUT("/admins/:id", middleware.RootAuth(), controller.UpdateManagedAdminPermissions)
	}
}
