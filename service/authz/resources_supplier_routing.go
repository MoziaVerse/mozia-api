package authz

var SupplierRoutingPublish = Permission{Resource: "supplier_routing", Action: "publish"}

func init() {
	RegisterResource(ResourceDefinition{Resource: "supplier_routing", LabelKey: "Supplier routing", Actions: []ActionDefinition{{Action: "publish", LabelKey: "Publish supplier routing", DescriptionKey: "Publish supplier capacity, routing policies and rollback revisions."}}})
}
