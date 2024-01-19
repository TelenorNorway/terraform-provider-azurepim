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
	msgraphsdk "github.com/microsoftgraph/msgraph-beta-sdk-go"
	"github.com/microsoftgraph/msgraph-beta-sdk-go/identitygovernance"
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

type Schedule struct {
	StartDateTime types.String `tfsdk:"start_date_time"`
	Expiration    Expiration   `tfsdk:"expiration"`
}

type Expiration struct {
	Type        types.String `tfsdk:"type"`
	EndDateTime types.String `tfsdk:"end_date_time"`
}

// GroupEligibleAssignmentModel describes the resource data model.
type GroupEligibleAssignmentModel struct {
	Id            types.String `tfsdk:"id"`
	Role          types.String `tfsdk:"role"`
	Scope         types.String `tfsdk:"scope"`
	Justification types.String `tfsdk:"justification"`
	PrincipalID   types.String `tfsdk:"principal_id"`
	Status        types.String `tfsdk:"status"`
	Schedule      Schedule     `tfsdk:"schedule"`
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
				MarkdownDescription: "Identifier",
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
		},
		Blocks: map[string]schema.Block{
			"schedule": schema.SingleNestedBlock{
				Attributes: map[string]schema.Attribute{
					"start_date_time": schema.StringAttribute{
						Required: true,
						PlanModifiers: []planmodifier.String{
							stringplanmodifier.RequiresReplace(),
						},
					},
				},
				Blocks: map[string]schema.Block{
					"expiration": schema.SingleNestedBlock{
						Attributes: map[string]schema.Attribute{
							"type": schema.StringAttribute{
								Required: true,
								PlanModifiers: []planmodifier.String{
									stringplanmodifier.RequiresReplace(),
								},
							},
							"end_date_time": schema.StringAttribute{
								Required: true,
								PlanModifiers: []planmodifier.String{
									stringplanmodifier.RequiresReplace(),
								},
							},
						},
					},
				},
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

	requestBody := graphmodels.NewPrivilegedAccessGroupEligibilityScheduleRequest()

	accessId := graphmodels.MEMBER_PRIVILEGEDACCESSGROUPRELATIONSHIPS
	requestBody.SetAccessId(&accessId)

	principalId := data.PrincipalID.ValueString()
	requestBody.SetPrincipalId(&principalId)

	groupId := data.Scope.ValueString()
	requestBody.SetGroupId(&groupId)

	action := graphmodels.ADMINASSIGN_SCHEDULEREQUESTACTIONS
	requestBody.SetAction(&action)

	scheduleInfo := graphmodels.NewRequestSchedule()
	startDateTime, err := time.Parse(time.RFC3339, data.Schedule.StartDateTime.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to parse startDateTime")
		return
	}
	scheduleInfo.SetStartDateTime(&startDateTime)
	expiration := graphmodels.NewExpirationPattern()
	typ := graphmodels.AFTERDATETIME_EXPIRATIONPATTERNTYPE
	expiration.SetTypeEscaped(&typ)

	endDateTime, err := time.Parse(time.RFC3339, data.Schedule.Expiration.EndDateTime.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to parse endDateTime")
		return
	}

	expiration.SetEndDateTime(&endDateTime)
	scheduleInfo.SetExpiration(expiration)
	requestBody.SetScheduleInfo(scheduleInfo)
	justification := "Assign eligible request."
	requestBody.SetJustification(&justification)

	eligibilityScheduleRequests, err := r.graphClient.
		IdentityGovernance().
		PrivilegedAccess().
		Group().
		EligibilityScheduleRequests().
		Post(context.Background(), requestBody, nil)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create eligibility schedule requests")
		return
	}

	id := eligibilityScheduleRequests.GetTargetScheduleId()
	if id == nil {
		resp.Diagnostics.AddError("Client Error", "Unable to get eligibility schedule requests ID")
		return
	}
	data.Id = types.StringValue(*id)

	status := eligibilityScheduleRequests.GetStatus()
	if status == nil {
		resp.Diagnostics.AddError("Client Error", "Unable to get eligibility schedule requests status")
		return
	}
	data.Status = types.StringValue(*status)

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

	requestFilter := fmt.Sprintf(
		"groupId eq '%s' and principalId eq '%s' and accessId eq '%s'",
		data.Scope.ValueString(),
		data.PrincipalID.ValueString(),
		data.Role.ValueString(),
	)

	groupEligibleResp, err := r.graphClient.
		IdentityGovernance().
		PrivilegedAccess().
		Group().
		EligibilityScheduleRequests().
		Get(context.Background(), &identitygovernance.PrivilegedAccessGroupEligibilityScheduleRequestsRequestBuilderGetRequestConfiguration{
			QueryParameters: &identitygovernance.PrivilegedAccessGroupEligibilityScheduleRequestsRequestBuilderGetQueryParameters{
				Filter: &requestFilter,
			},
		})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to get eligibility schedule requests")
		return
	}

	fmt.Println("read response", groupEligibleResp)

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

	tflog.Error(ctx, "update not implemented")

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

	tflog.Error(ctx, "delete not implemented")
}

func (r *GroupEligibleAssignment) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
