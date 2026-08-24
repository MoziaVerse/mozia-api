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
	resellerRoute.POST("/presentation", controller.GetResellerPresentation)

	resellerManagementRoute := apiRouter.Group("/internal/v1/reseller/management")
	resellerManagementRoute.Use(middleware.ResellerManagementServiceAuth())
	resellerManagementRoute.GET("/profile", controller.GetResellerManagementProfile)
	resellerManagementRoute.PUT("/logo", controller.UpdateResellerManagementLogo)
	resellerManagementRoute.PUT("/payment/bank-transfer", controller.UpdateResellerManagementBankTransfer)
	resellerManagementRoute.GET("/members", controller.ListResellerManagementMembers)
	resellerManagementRoute.POST("/members/subagents", controller.CreateResellerManagementSubagent)
	resellerManagementRoute.GET("/customers", controller.ListResellerManagementCustomers)
	resellerManagementRoute.GET("/customers/:id", controller.GetResellerManagementCustomer)
	resellerManagementRoute.PATCH("/customers/:id/status", controller.UpdateResellerManagementCustomerStatus)
	resellerManagementRoute.PATCH("/customers/:id/overseas-model-access", controller.UpdateResellerManagementCustomerOverseasModelAccess)
	resellerManagementRoute.PATCH("/customers/:id/reseller-payment", controller.UpdateResellerManagementCustomerPaymentPreference)
	resellerManagementRoute.PATCH("/customers/:id/remark", controller.UpdateResellerManagementCustomerRemark)
	resellerManagementRoute.PATCH("/customers/:id/subagent", controller.UpdateResellerManagementCustomerSubagent)
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
	resellerRegistrationRoute.POST("/customers/payment-method", controller.GetResellerRegistrationCustomerPaymentMethod)

	resellerAdminRoute := apiRouter.Group("/internal/v1/platform/resellers")
	resellerAdminRoute.Use(middleware.ResellerAdminServiceAuth())
	resellerAdminRoute.GET("", controller.ListResellerAdminRecords)
	resellerAdminRoute.POST("", controller.CreateResellerAdmin)
	resellerAdminRoute.PATCH("/:id", controller.UpdateResellerAdmin)
	resellerAdminRoute.PUT("/:id/logo", controller.UpdateResellerAdminLogo)
	resellerAdminRoute.PUT("/:id/payment/bank-transfer", controller.UpdateResellerAdminBankTransfer)
	resellerAdminRoute.PATCH("/:id/status", controller.UpdateResellerAdminStatus)
	resellerAdminRoute.GET("/:id/customers", controller.ListResellerAdminCustomers)
	resellerAdminRoute.DELETE("/:id/customers/:customer_id", controller.UnbindResellerAdminCustomer)
	resellerAdminRoute.POST("/:id/customers/batch-assign", controller.BatchAssignResellerAdminCustomers)
	resellerAdminRoute.GET("/hdu-identity-route", controller.GetHduResellerIdentityRoute)
	resellerAdminRoute.PUT("/hdu-identity-route", controller.UpsertHduResellerIdentityRoute)
	resellerAdminRoute.GET("/assignment-conflicts", controller.ListResellerAssignmentConflicts)
	resellerAdminRoute.POST("/assignment-conflicts/:id/resolve", controller.ResolveResellerAssignmentConflict)
	resellerAdminRoute.GET("/:id/pricing", controller.GetResellerPlatformPricing)
	resellerAdminRoute.POST("/:id/pricing/wholesale", controller.CreateResellerPlatformWholesalePrice)
	resellerAdminRoute.POST("/:id/pricing/preview", controller.PreviewResellerPlatformPricing)

}
