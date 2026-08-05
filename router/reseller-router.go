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
	resellerManagementRoute.GET("/invitations", controller.ListResellerManagementInvitations)
	resellerManagementRoute.POST("/invitations", controller.CreateResellerManagementInvitation)
	resellerManagementRoute.POST("/invitations/:id/revoke", controller.RevokeResellerManagementInvitation)

	resellerRegistrationRoute := apiRouter.Group("/internal/v1/reseller/registration")
	resellerRegistrationRoute.Use(middleware.ResellerRegistrationServiceAuth())
	resellerRegistrationRoute.POST("/invitations/consume", controller.ConsumeResellerRegistrationInvitation)

	resellerAdminRoute := apiRouter.Group("/internal/v1/platform/resellers")
	resellerAdminRoute.Use(middleware.ResellerAdminServiceAuth())
	resellerAdminRoute.GET("", controller.ListResellerAdminRecords)
	resellerAdminRoute.POST("", controller.CreateResellerAdmin)
	resellerAdminRoute.PATCH("/:id/status", controller.UpdateResellerAdminStatus)
	resellerAdminRoute.GET("/:id/customers", controller.ListResellerAdminCustomers)

	resellerPlatformCustomerRoute := apiRouter.Group("/internal/v1/platform/reseller-customers")
	resellerPlatformCustomerRoute.Use(middleware.ResellerAdminServiceAuth())
	resellerPlatformCustomerRoute.POST("/:id/transfer", controller.TransferResellerAdminCustomer)
}
