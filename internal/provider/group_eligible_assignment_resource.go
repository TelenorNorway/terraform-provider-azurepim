// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/microsoft/kiota-abstractions-go/serialization"
	msgraphsdk "github.com/microsoftgraph/msgraph-beta-sdk-go"
	graphmodels "github.com/microsoftgraph/msgraph-beta-sdk-go/models"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &GroupEligibleAssignment{}
var _ resource.ResourceWithImportState = &GroupEligibleAssignment{}

func NewGroupEligibleAssignment() resource.Resource {
	return &GroupEligibleAssignment{}
}

// GroupEligibleAssignment defines the resource implementation.
type GroupEligibleAssignment struct {
	graphClient *msgraphsdk.GraphServiceClient
}

// GroupEligibleAssignmentModel describes the resource data model.
type GroupEligibleAssignmentModel struct {
	Id            types.String `tfsdk:"id"`
	Role          types.String `tfsdk:"role"`
	Scope         types.String `tfsdk:"scope"`
	Justification types.String `tfsdk:"justification"`
	PrincipalID   types.String `tfsdk:"principal_id"`
	Status        types.String `tfsdk:"status"`
	StartDateTime types.String `tfsdk:"start_date_time"`
}

func (r *GroupEligibleAssignment) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group_eligible_assignment"
}

func (r *GroupEligibleAssignment) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: "Manages an Entra Group PIM Eligible Role Assignment.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The ID of the resource is the targetScheduleId value.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"role": schema.StringAttribute{
				// The equivalent of accessId in the SDK
				MarkdownDescription: "The role in which the principal can assume.",
				Required:            true,
				Validators:          []validator.String{stringvalidator.OneOf("owner", "member")},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"scope": schema.StringAttribute{
				// The equivalent of groupId in the SDK
				MarkdownDescription: "The target group of which the principal ID can assume a role.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"justification": schema.StringAttribute{
				MarkdownDescription: "A message provided by users and administrators when they create an assignment.",
				Optional:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"principal_id": schema.StringAttribute{
				MarkdownDescription: "The identifier of the principal whose membership or ownership eligibility to the group is managed through PIM for groups.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"status": schema.StringAttribute{
				Computed: true,
			},
			"start_date_time": schema.StringAttribute{
				Computed: true,
			},
		},
	}
}

func (r *GroupEligibleAssignment) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	creds, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create credentials")
		return
	}

	graphClient, err := msgraphsdk.NewGraphServiceClientWithCredentials(creds, nil)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create graph client")
		return
	}

	r.graphClient = graphClient
}

func (r *GroupEligibleAssignment) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data GroupEligibleAssignmentModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	data.StartDateTime = types.StringValue(time.Now().Format(time.RFC3339))

	requestBody, err := newPrivilegedAccessGroupEligibilityScheduleRequest(data)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create eligibility schedule requests: "+err.Error())
		return
	}

	eligibilityScheduleRequests, err := r.graphClient.
		IdentityGovernance().
		PrivilegedAccess().
		Group().
		EligibilityScheduleRequests().
		Post(context.Background(), requestBody, nil)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create eligibility schedule requests: "+err.Error())
		return
	}

	data.Id = types.StringValue(*eligibilityScheduleRequests.GetId())

	status := eligibilityScheduleRequests.GetStatus()
	if status == nil {
		resp.Diagnostics.AddError("Client Error", "Unable to get eligibility schedule requests status")
		return
	}
	data.Status = types.StringValue(*status)
	data.Justification = types.StringValue(*eligibilityScheduleRequests.GetJustification())
	data.PrincipalID = types.StringValue(*eligibilityScheduleRequests.GetPrincipalId())
	role, err := convertAccessIdToRole(*eligibilityScheduleRequests.GetAccessId())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to convert access ID to role: "+err.Error())
		return
	}
	data.Role = types.StringValue(role)
	data.Scope = types.StringValue(*eligibilityScheduleRequests.GetGroupId())
	data.StartDateTime = types.StringValue(eligibilityScheduleRequests.GetScheduleInfo().GetStartDateTime().Format(time.RFC3339))

	tflog.Trace(ctx, "created a resource")

	// Save data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GroupEligibleAssignment) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data GroupEligibleAssignmentModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	groupEligibleResp, err := r.graphClient.
		IdentityGovernance().
		PrivilegedAccess().
		Group().
		EligibilityScheduleRequests().
		ByPrivilegedAccessGroupEligibilityScheduleRequestId(data.Id.ValueString()).
		Get(context.Background(), nil)
	if err != nil {
		resp.Diagnostics.AddError("Client call failed", "Unable to get eligibility schedule requests: "+err.Error())
		return
	}

	data.Id = types.StringValue(*groupEligibleResp.GetId())
	data.Justification = types.StringValue(*groupEligibleResp.GetJustification())
	data.Status = types.StringValue(*groupEligibleResp.GetStatus())
	data.PrincipalID = types.StringValue(*groupEligibleResp.GetPrincipalId())

	role, err := convertAccessIdToRole(*groupEligibleResp.GetAccessId())
	if err != nil {
		resp.Diagnostics.AddError("Conversion failed", "Unable to convert access ID to role: "+err.Error())
		return
	}
	data.Role = types.StringValue(role)

	data.Scope = types.StringValue(*groupEligibleResp.GetGroupId())
	data.StartDateTime = types.StringValue(groupEligibleResp.GetScheduleInfo().GetStartDateTime().Format(time.RFC3339))

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GroupEligibleAssignment) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data GroupEligibleAssignmentModel

	// Read Terraform plan data into the model
	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Info(ctx, "resource can only be replaced")

	// Save updated data into Terraform state
	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *GroupEligibleAssignment) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data GroupEligibleAssignmentModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	requestBody, err := newPrivilegedAccessGroupEligibilityScheduleRequest(data)
	if err != nil {
		resp.Diagnostics.AddError("Error deleting resource", "Unable to create eligibility schedule request: "+err.Error())
		return
	}

	requestBody.SetAction(toPtr(graphmodels.ADMINREMOVE_SCHEDULEREQUESTACTIONS))
	requestBody.SetId(toPtr(data.Id.ValueString()))

	_, err = r.graphClient.
		IdentityGovernance().
		PrivilegedAccess().
		Group().
		EligibilityScheduleRequests().
		Post(ctx, requestBody, nil)

	if err != nil {
		resp.Diagnostics.AddError("Error deleting resource", "Unable to delete eligibility schedule request: "+err.Error())
		return
	}
}

func (r *GroupEligibleAssignment) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

func newPrivilegedAccessGroupEligibilityScheduleRequest(data GroupEligibleAssignmentModel) (*graphmodels.PrivilegedAccessGroupEligibilityScheduleRequest, error) {
	requestBody := graphmodels.NewPrivilegedAccessGroupEligibilityScheduleRequest()

	accessId, err := convertRoleToAccessId(data.Role.ValueString())
	if err != nil {
		return nil, fmt.Errorf("unable to convert role to access ID: %w", err)
	}

	requestBody.SetAccessId(&accessId)

	principalId := data.PrincipalID.ValueString()
	requestBody.SetPrincipalId(&principalId)

	groupId := data.Scope.ValueString()
	requestBody.SetGroupId(&groupId)

	action := graphmodels.ADMINASSIGN_SCHEDULEREQUESTACTIONS
	requestBody.SetAction(&action)

	scheduleInfo := graphmodels.NewRequestSchedule()
	startDateTime, err := time.Parse(time.RFC3339, data.StartDateTime.ValueString())
	if err != nil {
		return nil, fmt.Errorf("unable to parse startDateTime: %w", err)
	}

	scheduleInfo.SetStartDateTime(&startDateTime)
	expiration := graphmodels.NewExpirationPattern()
	typ := graphmodels.AFTERDURATION_EXPIRATIONPATTERNTYPE
	expiration.SetTypeEscaped(&typ)
	dur := 180 * 24 * time.Hour
	expiration.SetDuration(serialization.FromDuration(dur))

	scheduleInfo.SetExpiration(expiration)
	requestBody.SetScheduleInfo(scheduleInfo)
	requestBody.SetJustification(toPtr(data.Justification.ValueString()))

	return requestBody, nil
}

func convertRoleToAccessId(role string) (graphmodels.PrivilegedAccessGroupRelationships, error) {
	switch role {
	case "owner":
		return graphmodels.OWNER_PRIVILEGEDACCESSGROUPRELATIONSHIPS, nil
	case "member":
		return graphmodels.MEMBER_PRIVILEGEDACCESSGROUPRELATIONSHIPS, nil
	default:
		return 0, fmt.Errorf("invalid role: %s", role)
	}
}

func convertAccessIdToRole(accessId graphmodels.PrivilegedAccessGroupRelationships) (string, error) {
	switch accessId {
	case graphmodels.OWNER_PRIVILEGEDACCESSGROUPRELATIONSHIPS:
		return "owner", nil
	case graphmodels.MEMBER_PRIVILEGEDACCESSGROUPRELATIONSHIPS:
		return "member", nil
	default:
		return "", fmt.Errorf("invalid accessId: %d", accessId)
	}
}

func toPtr[T any](v T) *T {
	return &v
}
