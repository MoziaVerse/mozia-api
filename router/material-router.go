package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-gonic/gin"
)

func SetMaterialRouter(router *gin.Engine) {
	materialRouter := router.Group("/v1/sd")
	materialRouter.Use(middleware.RouteTag("relay"))
	materialRouter.Use(middleware.SystemPerformanceCheck())
	materialRouter.Use(middleware.TokenAuth())
	materialRouter.Use(middleware.UploadRateLimit())
	{
		materialRouter.POST("/upload", controller.UploadMaterial)
		materialRouter.POST("/upload_url", controller.ImportMaterial)
	}
}
