package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

func registerResellerRoutes(apiRouter *gin.RouterGroup) {
	resellerRoute := apiRouter.Group("/internal/v1/reseller")
	resellerRoute.Use(middleware.ResellerServiceAuth())
	resellerRoute.POST("/context", controller.GetResellerContext)

	resellerManagementRoute := apiRouter.Group("/internal/v1/reseller/management")
	resellerManagementRoute.Use(middleware.ResellerManagementServiceAuth())
	resellerManagementRoute.GET("/profile", controller.GetResellerManagementProfile)
	resellerManagementRoute.GET("/members", controller.ListResellerManagementMembers)
	resellerManagementRoute.GET("/customers", controller.ListResellerManagementCustomers)
	resellerManagementRoute.GET("/customers/:id", controller.GetResellerManagementCustomer)
	resellerManagementRoute.PATCH("/customers/:id/status", controller.UpdateResellerManagementCustomerStatus)
	resellerManagementRoute.PATCH("/customers/:id/remark", controller.UpdateResellerManagementCustomerRemark)
	resellerManagementRoute.GET("/invitations", controller.ListResellerManagementInvitations)
	resellerManagementRoute.POST("/invitations", controller.CreateResellerManagementInvitation)
	resellerManagementRoute.POST("/invitations/:id/revoke", controller.RevokeResellerManagementInvitation)
	resellerManagementRoute.GET("/pricing", controller.GetResellerManagementPricing)
	resellerManagementRoute.POST("/pricing/retail", controller.CreateResellerManagementRetailPrice)
	resellerManagementRoute.POST("/pricing/preview", controller.PreviewResellerManagementPricing)
	resellerManagementRoute.GET("/usage", controller.GetResellerManagementUsage)
	resellerManagementRoute.GET("/tasks", controller.GetResellerManagementTasks)

	resellerRegistrationRoute := apiRouter.Group("/internal/v1/reseller/registration")
	resellerRegistrationRoute.Use(middleware.ResellerRegistrationServiceAuth())
	resellerRegistrationRoute.POST("/invitations/consume", controller.ConsumeResellerRegistrationInvitation)
	resellerRegistrationRoute.POST("/customers/profile", controller.SyncResellerRegistrationCustomerIdentity)
	resellerRegistrationRoute.GET("/customers/pending-profiles", controller.ListPendingResellerRegistrationCustomerProfiles)

	resellerAdminRoute := apiRouter.Group("/internal/v1/platform/resellers")
	resellerAdminRoute.Use(middleware.ResellerAdminServiceAuth())
	resellerAdminRoute.GET("", controller.ListResellerAdminRecords)
	resellerAdminRoute.POST("", controller.CreateResellerAdmin)
	resellerAdminRoute.PATCH("/:id", controller.UpdateResellerAdmin)
	resellerAdminRoute.PATCH("/:id/status", controller.UpdateResellerAdminStatus)
	resellerAdminRoute.GET("/:id/customers", controller.ListResellerAdminCustomers)
	resellerAdminRoute.DELETE("/:id/customers/:customer_id", controller.UnbindResellerAdminCustomer)
	resellerAdminRoute.POST("/:id/customers/batch-assign", controller.BatchAssignResellerAdminCustomers)
	resellerAdminRoute.GET("/:id/pricing", controller.GetResellerPlatformPricing)
	resellerAdminRoute.POST("/:id/pricing/wholesale", controller.CreateResellerPlatformWholesalePrice)
	resellerAdminRoute.POST("/:id/pricing/preview", controller.PreviewResellerPlatformPricing)

}
