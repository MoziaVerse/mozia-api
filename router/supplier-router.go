package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
)

func registerSupplierRoutes(api *gin.RouterGroup) {
	group := api.Group("")
	group.Use(middleware.AdminAuth())
	read := middleware.RequirePermission(authz.ChannelRead)
	write := middleware.RequirePermission(authz.SupplierRoutingPublish)
	for _, resource := range []struct{ kind, list, detail string }{
		{"supplier", "/suppliers", "/suppliers/:id"},
		{"pool", "/suppliers/:id/pools", "/supplier-pools/:id"},
		{"binding", "/supplier-bindings", "/supplier-bindings/:id"},
		{"rule", "/supplier-routing/rules", "/supplier-routing/rules/:id"},
	} {
		group.GET(resource.list, read, controller.SupplierResourceList(resource.kind))
		group.POST(resource.list, write, controller.SupplierResourceWrite(resource.kind))
		group.GET(resource.detail, read, controller.SupplierResourceDetail(resource.kind))
		group.PATCH(resource.detail, write, controller.SupplierResourceWrite(resource.kind))
		group.DELETE(resource.detail, write, controller.SupplierResourceWrite(resource.kind))
	}
	group.GET("/supplier-routing/settings", read, controller.SupplierResourceDetail("settings"))
	group.PATCH("/supplier-routing/settings", write, controller.SupplierResourceWrite("settings"))
	group.POST("/supplier-routing/settings/restore", write, controller.SupplierResourceWrite("settings"))
	group.POST("/supplier-routing/rules/:id/restore", write, controller.SupplierResourceWrite("rule"))
	group.GET("/supplier-routing/revisions", read, controller.GetSupplierResourceRevisions)
	group.GET("/supplier-routing/revisions/:id", read, controller.GetSupplierResourceRevision)
	group.GET("/supplier-routing/status", read, controller.GetSupplierResourceStatus)
}
