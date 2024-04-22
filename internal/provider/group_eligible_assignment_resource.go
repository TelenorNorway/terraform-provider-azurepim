// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	azcorepolicy "github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
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
	graphpolicies "github.com/microsoftgraph/msgraph-beta-sdk-go/policies"
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
	Id                   types.String `tfsdk:"id"`
	Role                 types.String `tfsdk:"role"`
	Scope                types.String `tfsdk:"scope"`
	Justification        types.String `tfsdk:"justification"`
	PrincipalID          types.String `tfsdk:"principal_id"`
	Status               types.String `tfsdk:"status"`
	StartDateTime        types.String `tfsdk:"start_date_time"`
	EligibleAssignmentID types.String `tfsdk:"eligible_assignment_id"`
}

func (r *GroupEligibleAssignment) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group_eligible_assignment"
}

func (r *GroupEligibleAssignment) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		// This description is used by the documentation generator and the language server.
		MarkdownDescription: `
Enables PIM for an Entra group, manages an PIM Eligible Role Assignment and sets the PIM policy for the member role to allow for no expiration on eligible assignments.

It requires the following graph permissions:
- PrivilegedEligibilitySchedule.ReadWrite.AzureADGroup
- RoleManagementPolicy.ReadWrite.AzureADGroup

The resource does not support all the available configuration options for PIM Eligible Role Assignment for groups and its associated policy. 
`,

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The ID of the resource is the '{scope}|{principal_id}' value.",
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
			"eligible_assignment_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The ID of the eligibility schedule request.",
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

	policyId, err := r.getEligibleExpirationPolicyId(ctx, data.Scope.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Graph client error", "Unable to get eligible expiration policy ID: "+err.Error())
		return
	}

	if err := r.updateUnifiedRoleManagementPolicyRule(ctx, policyId, false); err != nil {
		resp.Diagnostics.AddError("Graph client error", "Unable to update unified role management policy rule: "+err.Error())
		return
	}

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
		Post(ctx, requestBody, nil)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", "Unable to create eligibility schedule requests: "+err.Error())
		return
	}

	data.Id = types.StringValue(fmt.Sprintf("%s|%s", *eligibilityScheduleRequests.GetGroupId(), *eligibilityScheduleRequests.GetPrincipalId()))

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

func (r *GroupEligibleAssignment) getEligibleExpirationPolicyId(ctx context.Context, scope string) (string, error) {
	requestFilter := fmt.Sprintf("scopeId eq '%s' and scopeType eq 'Group' and roleDefinitionId eq 'member'", scope)

	roleManagementPolicyAssignments, err := r.graphClient.
		Policies().
		RoleManagementPolicyAssignments().
		Get(ctx, &graphpolicies.RoleManagementPolicyAssignmentsRequestBuilderGetRequestConfiguration{
			QueryParameters: &graphpolicies.RoleManagementPolicyAssignmentsRequestBuilderGetQueryParameters{
				Filter: &requestFilter,
				Expand: []string{"policy($expand=rules)"},
			},
		})

	if err != nil {
		return "", fmt.Errorf("unable to get role management policy assignments: %w", err)
	}

	// Edit the policy group assignment and allow no expiration date for PIM eligible assignment
	policyAssignments := roleManagementPolicyAssignments.GetValue()
	if len(policyAssignments) == 0 {
		return "", fmt.Errorf("unable to find role management policy assignments from result")
	}

	if len(policyAssignments) > 1 {
		tflog.Warn(ctx, "found more than one role management policy assignment")
	}

	return *policyAssignments[0].GetPolicyId(), nil
}

// updateUnifiedRoleManagementPolicyRule had to be implemented without SDK because the SDK data model for this endpoint had several missing fields.
func (r *GroupEligibleAssignment) updateUnifiedRoleManagementPolicyRule(ctx context.Context, policyId string, isExpirationRequired bool) error {

	creds, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("unable to create credentials: %w", err)
	}

	t, err := creds.GetToken(ctx, azcorepolicy.TokenRequestOptions{Scopes: []string{"https://graph.microsoft.com/.default"}})
	if err != nil {
		return fmt.Errorf("unable to get token: %w", err)
	}

	c := &http.Client{
		Timeout: 10 * time.Second,
	}

	type target struct {
		Caller              string   `json:"caller"`
		Operations          []string `json:"operations"`
		Level               string   `json:"level"`
		InheritableSettings []any    `json:"inheritableSettings"`
		EnforcedSettings    []any    `json:"enforcedSettings"`
	}

	type policyRule struct {
		OdataType            string `json:"@odata.type"`
		ID                   string `json:"id"`
		IsExpirationRequired bool   `json:"isExpirationRequired"`
		MaximumDuration      string `json:"maximumDuration"`
		Target               target `json:"target"`
	}

	pr := policyRule{
		OdataType:            "#microsoft.graph.unifiedRoleManagementPolicyExpirationRule",
		ID:                   "Expiration_Admin_Eligibility",
		IsExpirationRequired: isExpirationRequired,
		MaximumDuration:      "P365D",
		Target: target{
			Caller:              "Admin",
			Operations:          []string{"All"},
			Level:               "Eligibility",
			EnforcedSettings:    []any{},
			InheritableSettings: []any{},
		},
	}

	b, err := json.Marshal(pr)
	if err != nil {
		return fmt.Errorf("unable to marshal body: %w", err)
	}

	req, err := http.NewRequest(http.MethodPatch, fmt.Sprintf("https://graph.microsoft.com/beta/policies/roleManagementPolicies/%s/rules/Expiration_Admin_Eligibility", policyId), bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("unable to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", t.Token))
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("unable to send request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("unable to read response body: %w", err)
		}
		defer req.Body.Close()

		return fmt.Errorf("unable to update unified role management policy rule, got %d want %d: %s", resp.StatusCode, http.StatusOK, string(b))
	}

	return nil
}

func (r *GroupEligibleAssignment) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data GroupEligibleAssignmentModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	idSplit := strings.Split(data.Id.ValueString(), "|")
	if len(idSplit) != 2 {
		resp.Diagnostics.AddError("Invalid ID", "ID must be in the format '{scope}|{principal_id}'")
		return
	}

	scope, principalID := idSplit[0], idSplit[1]
	filter := toPtr(fmt.Sprintf("groupId eq '%s' and principalId eq '%s'", scope, principalID))
	groupEligibleResp, err := r.graphClient.
		IdentityGovernance().
		PrivilegedAccess().
		Group().
		EligibilityScheduleRequests().
		Get(ctx, &identitygovernance.PrivilegedAccessGroupEligibilityScheduleRequestsRequestBuilderGetRequestConfiguration{
			QueryParameters: &identitygovernance.PrivilegedAccessGroupEligibilityScheduleRequestsRequestBuilderGetQueryParameters{
				Filter: filter,
			},
		})
	if err != nil {
		resp.Diagnostics.AddError("Client call failed", fmt.Sprintf("Unable to get eligibility schedule requests with filter '%s': %s", *filter, err.Error()))
		return
	}

	groupEligibles := groupEligibleResp.GetValue()
	if len(groupEligibles) != 1 {
		resp.Diagnostics.AddError("Client call failed", fmt.Sprintf("Got %d results, want 1", len(groupEligibles)))
		return
	}
	groupEligible := groupEligibles[0]

	data.EligibleAssignmentID = types.StringValue(*groupEligible.GetId())
	data.Justification = types.StringValue(*groupEligible.GetJustification())
	data.Status = types.StringValue(*groupEligible.GetStatus())
	data.PrincipalID = types.StringValue(*groupEligible.GetPrincipalId())

	role, err := convertAccessIdToRole(*groupEligible.GetAccessId())
	if err != nil {
		resp.Diagnostics.AddError("Conversion failed", "Unable to convert access ID to role: "+err.Error())
		return
	}
	data.Role = types.StringValue(role)

	data.Scope = types.StringValue(*groupEligible.GetGroupId())
	data.StartDateTime = types.StringValue(groupEligible.GetScheduleInfo().GetStartDateTime().Format(time.RFC3339))

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
	requestBody.SetId(toPtr(data.EligibleAssignmentID.ValueString()))

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

	policyId, err := r.getEligibleExpirationPolicyId(ctx, data.Scope.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Graph client error", "Unable to get eligible expiration policy ID: "+err.Error())
		return
	}

	if err := r.updateUnifiedRoleManagementPolicyRule(ctx, policyId, true); err != nil {
		resp.Diagnostics.AddError("Graph client error", "Unable to update unified role management policy rule: "+err.Error())
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
	typ := graphmodels.NOEXPIRATION_EXPIRATIONPATTERNTYPE
	expiration.SetTypeEscaped(&typ)

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
