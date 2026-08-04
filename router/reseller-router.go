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
}
