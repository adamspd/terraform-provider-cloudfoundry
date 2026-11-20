package provider

import (
	"context"
	"fmt"

	cfv3client "github.com/cloudfoundry/go-cfclient/v3/client"
	cfv3resource "github.com/cloudfoundry/go-cfclient/v3/resource"
	"github.com/cloudfoundry/terraform-provider-cloudfoundry/cloudfoundry/provider/managers"
	"github.com/cloudfoundry/terraform-provider-cloudfoundry/internal/validation"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// defaultOrgQuotaName is the name of the quota Cloud Foundry applies to new orgs
// unless the platform operator configured another one.
const defaultOrgQuotaName = "default"

var (
	_ resource.Resource                = &orgQuotaAssignmentResource{}
	_ resource.ResourceWithConfigure   = &orgQuotaAssignmentResource{}
	_ resource.ResourceWithImportState = &orgQuotaAssignmentResource{}
	_ resource.ResourceWithIdentity    = &orgQuotaAssignmentResource{}
)

func NewOrgQuotaAssignmentResource() resource.Resource {
	return &orgQuotaAssignmentResource{}
}

type orgQuotaAssignmentResource struct {
	cfClient *cfv3client.Client
}

type orgQuotaAssignmentIdentityModel struct {
	OrgGUID types.String `tfsdk:"org_guid"`
}

func (r *orgQuotaAssignmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_org_quota_assignment"
}

func (r *orgQuotaAssignmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Assigns an org quota to an org. " +
			"Use this resource instead of the `orgs` attribute of `cloudfoundry_org_quota` when the quota and the org are created and destroyed together: " +
			"the assignment is destroyed first, so the quota can be deleted before the org. " +
			"Do not manage the same org with both this resource and the `orgs` attribute of `cloudfoundry_org_quota`.\n\n" +
			"An org always has exactly one quota. When this resource is destroyed, the org is moved to the `fallback_quota`.",
		Attributes: map[string]schema.Attribute{
			"org": schema.StringAttribute{
				MarkdownDescription: "The GUID of the org the quota is assigned to. Changing it creates a new assignment.",
				Required:            true,
				Validators: []validator.String{
					validation.ValidUUID(),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"quota": schema.StringAttribute{
				MarkdownDescription: "The GUID of the org quota to assign to the org.",
				Required:            true,
				Validators: []validator.String{
					validation.ValidUUID(),
				},
			},
			"fallback_quota": schema.StringAttribute{
				MarkdownDescription: "The GUID of the org quota the org is moved to when this resource is destroyed. " +
					"If not set, the quota named `" + defaultOrgQuotaName + "` is used.",
				Optional: true,
				Validators: []validator.String{
					validation.ValidUUID(),
				},
			},
			idKey: schema.StringAttribute{
				MarkdownDescription: "The GUID of the org. It is the identifier of this assignment.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *orgQuotaAssignmentResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"org_guid": identityschema.StringAttribute{
				RequiredForImport: true,
			},
		},
	}
}

func (r *orgQuotaAssignmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	session, ok := req.ProviderData.(*managers.Session)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *managers.Session, got: %T. Please report this issue to the provider developers", req.ProviderData),
		)
		return
	}
	r.cfClient = session.CFClient
}

func (r *orgQuotaAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan orgQuotaAssignmentType
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if _, err := r.cfClient.OrganizationQuotas.Apply(ctx, plan.Quota.ValueString(), []string{plan.Org.ValueString()}); err != nil {
		resp.Diagnostics.AddError(
			"Unable to assign org quota to org",
			fmt.Sprintf("Request failed with %s", err.Error()),
		)
		return
	}

	plan.ID = plan.Org
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	resp.Diagnostics.Append(resp.Identity.Set(ctx, orgQuotaAssignmentIdentityModel{OrgGUID: plan.Org})...)
}

func (r *orgQuotaAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state orgQuotaAssignmentType
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The id is the org GUID, which also covers the import case.
	org, err := r.cfClient.Organizations.Get(ctx, state.ID.ValueString())
	if err != nil {
		handleReadErrors(ctx, resp, err, "org quota assignment", state.ID.ValueString())
		return
	}

	state.Org = types.StringValue(org.GUID)
	state.Quota = types.StringValue(org.Relationships.Quota.Data.GUID)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)

	var identity orgQuotaAssignmentIdentityModel
	if req.Identity.Get(ctx, &identity).HasError() {
		resp.Diagnostics.Append(resp.Identity.Set(ctx, orgQuotaAssignmentIdentityModel{OrgGUID: state.Org})...)
	}
}

func (r *orgQuotaAssignmentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state orgQuotaAssignmentType
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if plan.Quota.ValueString() != state.Quota.ValueString() {
		if _, err := r.cfClient.OrganizationQuotas.Apply(ctx, plan.Quota.ValueString(), []string{plan.Org.ValueString()}); err != nil {
			resp.Diagnostics.AddError(
				"Unable to assign org quota to org",
				fmt.Sprintf("Request failed with %s", err.Error()),
			)
			return
		}
	}

	plan.ID = plan.Org
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	// WORKAROUND for OpenTofu compatibility
	// https://github.com/cloudfoundry/terraform-provider-cloudfoundry/issues/418
	resp.Diagnostics.Append(resp.Identity.Set(ctx, orgQuotaAssignmentIdentityModel{OrgGUID: plan.Org})...)
	// END WORKAROUND
}

func (r *orgQuotaAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state orgQuotaAssignmentType
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	org, err := r.cfClient.Organizations.Get(ctx, state.Org.ValueString())
	if err != nil {
		if cfv3resource.IsResourceNotFoundError(err) {
			// The org is already gone, so there is nothing to move.
			return
		}
		resp.Diagnostics.AddError(
			"Unable to read org while removing the org quota assignment",
			fmt.Sprintf("Request failed with %s", err.Error()),
		)
		return
	}
	if org.Relationships.Quota.Data.GUID != state.Quota.ValueString() {
		// The org was moved to another quota outside of Terraform, so this quota is already released.
		return
	}

	fallback := state.FallbackQuota.ValueString()
	if state.FallbackQuota.IsNull() || fallback == "" {
		opts := cfv3client.NewOrganizationQuotaListOptions()
		opts.Names = cfv3client.Filter{Values: []string{defaultOrgQuotaName}}
		defaultQuota, err := r.cfClient.OrganizationQuotas.Single(ctx, opts)
		if err != nil {
			resp.Diagnostics.AddError(
				"Unable to find the fallback org quota",
				fmt.Sprintf("The org must be moved to another quota before the assignment can be removed, "+
					"but no org quota named %q was found (%s). Set the attribute `fallback_quota`.", defaultOrgQuotaName, err.Error()),
			)
			return
		}
		fallback = defaultQuota.GUID
	}
	if fallback == state.Quota.ValueString() {
		return
	}

	if _, err := r.cfClient.OrganizationQuotas.Apply(ctx, fallback, []string{state.Org.ValueString()}); err != nil {
		resp.Diagnostics.AddError(
			"Unable to move org to the fallback quota",
			fmt.Sprintf("Request failed with %s", err.Error()),
		)
	}
}

func (r *orgQuotaAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID != "" {
		resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
		return
	}
	resource.ImportStatePassthroughWithIdentity(ctx, path.Root("id"), path.Root("org_guid"), req, resp)
}
