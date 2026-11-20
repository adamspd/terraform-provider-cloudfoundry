package provider

import "github.com/hashicorp/terraform-plugin-framework/types"

type orgQuotaAssignmentType struct {
	ID            types.String `tfsdk:"id"`
	Org           types.String `tfsdk:"org"`
	Quota         types.String `tfsdk:"quota"`
	FallbackQuota types.String `tfsdk:"fallback_quota"`
}
