package authz

const (
	ResourceModelPricing = "model_pricing"
	ResourceUserManage   = "user_management"
	ResourceUserQuota    = "user_quota"
	ResourceUserRatio    = "user_ratio"
	ResourceQuotaPolicy  = "quota_policy"
	ResourceGeneralAdmin = "general_admin"
	ActionGroupWrite     = "group_write"
	ActionAccess         = "access"
)

var (
	ModelPricingRead  = Permission{Resource: ResourceModelPricing, Action: ActionRead}
	ModelPricingWrite = Permission{Resource: ResourceModelPricing, Action: ActionWrite}

	UserManageRead       = Permission{Resource: ResourceUserManage, Action: ActionRead}
	UserManageWrite      = Permission{Resource: ResourceUserManage, Action: ActionWrite}
	UserManageGroupWrite = Permission{Resource: ResourceUserManage, Action: ActionGroupWrite}

	UserQuotaRead  = Permission{Resource: ResourceUserQuota, Action: ActionRead}
	UserQuotaWrite = Permission{Resource: ResourceUserQuota, Action: ActionWrite}

	UserRatioRead  = Permission{Resource: ResourceUserRatio, Action: ActionRead}
	UserRatioWrite = Permission{Resource: ResourceUserRatio, Action: ActionWrite}

	QuotaPolicyRead  = Permission{Resource: ResourceQuotaPolicy, Action: ActionRead}
	QuotaPolicyWrite = Permission{Resource: ResourceQuotaPolicy, Action: ActionWrite}

	GeneralAdminAccess = Permission{Resource: ResourceGeneralAdmin, Action: ActionAccess}
)

func init() {
	RegisterResource(ResourceDefinition{
		Resource: ResourceModelPricing,
		LabelKey: "Model Pricing Management",
		Actions: []ActionDefinition{
			{Action: ActionRead, LabelKey: "Read model pricing", DescriptionKey: "View model prices and billing ratios."},
			{Action: ActionWrite, LabelKey: "Edit model pricing", DescriptionKey: "Change model prices and billing ratios."},
		},
	})
	RegisterResource(ResourceDefinition{
		Resource: ResourceUserManage,
		LabelKey: "User Management",
		Actions: []ActionDefinition{
			{Action: ActionRead, LabelKey: "Read users", DescriptionKey: "View user lists and account details.", DefaultRoles: []string{BuiltInRoleAdmin}},
			{Action: ActionWrite, LabelKey: "Edit users", DescriptionKey: "Create, update, enable, disable, or delete users.", DefaultRoles: []string{BuiltInRoleAdmin}},
			{Action: ActionGroupWrite, LabelKey: "Adjust user groups", DescriptionKey: "Change the group assigned to a user.", DefaultRoles: []string{BuiltInRoleAdmin}},
		},
	})
	RegisterResource(ResourceDefinition{
		Resource: ResourceUserQuota,
		LabelKey: "User Quota Management",
		Actions: []ActionDefinition{
			{Action: ActionRead, LabelKey: "Read user quota", DescriptionKey: "View user wallet and quota balances."},
			{Action: ActionWrite, LabelKey: "Adjust user quota", DescriptionKey: "Increase, decrease, or reconcile user quota balances."},
		},
	})
	RegisterResource(ResourceDefinition{
		Resource: ResourceUserRatio,
		LabelKey: "User Billing Ratio Management",
		Actions: []ActionDefinition{
			{Action: ActionRead, LabelKey: "Read user billing ratios", DescriptionKey: "View user-specific model and channel billing ratios."},
			{Action: ActionWrite, LabelKey: "Edit user billing ratios", DescriptionKey: "Create, update, or remove user-specific billing ratios."},
		},
	})
	RegisterResource(ResourceDefinition{
		Resource: ResourceQuotaPolicy,
		LabelKey: "Model Quota Policy Management",
		Actions: []ActionDefinition{
			{Action: ActionRead, LabelKey: "Read model quota policies", DescriptionKey: "View model quota-source policies."},
			{Action: ActionWrite, LabelKey: "Edit model quota policies", DescriptionKey: "Create, update, or remove model quota-source policies."},
		},
	})
	RegisterResource(ResourceDefinition{
		Resource: ResourceGeneralAdmin,
		LabelKey: "Other Administration",
		Actions: []ActionDefinition{
			{Action: ActionAccess, LabelKey: "Access other administration", DescriptionKey: "Use administrative features outside the operations permissions listed above.", DefaultRoles: []string{BuiltInRoleAdmin}},
		},
	})
}
