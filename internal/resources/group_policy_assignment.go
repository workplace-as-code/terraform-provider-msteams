package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/workplace-as-code/terraform-provider-msteams/internal/client"
)

var (
	_ resource.Resource                = &groupPolicyAssignmentResource{}
	_ resource.ResourceWithImportState = &groupPolicyAssignmentResource{}
)

// groupPolicyAssignmentResource assigns a policy to the members of a group
// (New-CsGroupPolicyAssignment). Teams has no cmdlet to change an assignment,
// so every attribute forces a new one.
type groupPolicyAssignmentResource struct {
	client *client.Client
}

type groupPolicyAssignmentModel struct {
	ID         types.String `tfsdk:"id"`
	GroupID    types.String `tfsdk:"group_id"`
	PolicyType types.String `tfsdk:"policy_type"`
	PolicyName types.String `tfsdk:"policy_name"`
	Rank       types.Int64  `tfsdk:"rank"`
}

type groupPolicyAssignmentAPIResponse struct {
	GroupID    string `json:"GroupId"`
	PolicyType string `json:"PolicyType"`
	PolicyName string `json:"PolicyName"`
	Rank       int64  `json:"Rank"`
}

// NewGroupPolicyAssignmentResource is registered in Resources().
func NewGroupPolicyAssignmentResource() resource.Resource {
	return &groupPolicyAssignmentResource{}
}

func (r *groupPolicyAssignmentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_group_policy_assignment"
}

func (r *groupPolicyAssignmentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	replace := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Assigns a Teams policy to the members of a group (`New-CsGroupPolicyAssignment`). " +
			"Members get the policy unless a policy of the same type is assigned to them directly, or through a group with a lower `rank`. " +
			"Teams cannot change an assignment in place: changing any attribute replaces it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "`<group_id>/<policy_type>`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"group_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Object ID of the Entra ID group: security group, distribution list or Microsoft 365 group.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers:       replace,
			},
			"policy_type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "Policy type, for example `TeamsMeetingPolicy`, `TeamsMessagingPolicy`, `TeamsCallingPolicy`, " +
					"`TeamsAppSetupPolicy`, `TeamsChannelsPolicy`, `TeamsUpdateManagementPolicy`, `TeamsFeedbackPolicy` or `TeamsEventsPolicy`. " +
					"A group can have one assignment per policy type.",
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers: []planmodifier.String{caseInsensitive(), stringplanmodifier.RequiresReplace()},
			},
			"policy_name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Name of the policy to assign, for example `msteams_meeting_policy.example.name`.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers:       []planmodifier.String{caseInsensitive(), stringplanmodifier.RequiresReplace()},
			},
			"rank": schema.Int64Attribute{
				Required: true,
				MarkdownDescription: "Priority of this assignment over the other group assignments of the same policy type. " +
					"`1` wins over every other rank.",
				Validators:    []validator.Int64{int64validator.AtLeast(1)},
				PlanModifiers: []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
		},
	}
}

func (r *groupPolicyAssignmentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if c := clientFromProviderData(req, resp); c != nil {
		r.client = c
	}
}

func (r *groupPolicyAssignmentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan groupPolicyAssignmentModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	output, err := r.client.RunScript(ctx, "group_policy_assignment.ps1", map[string]string{
		"Action":     "Create",
		"GroupId":    plan.GroupID.ValueString(),
		"PolicyType": plan.PolicyType.ValueString(),
		"PolicyName": plan.PolicyName.ValueString(),
		"Rank":       fmt.Sprintf("%d", plan.Rank.ValueInt64()),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to create group policy assignment", err.Error())
		return
	}
	state, err := groupPolicyAssignmentFromAPI(plan, output)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse PowerShell response", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *groupPolicyAssignmentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var prior groupPolicyAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	output, err := r.client.RunScript(ctx, "group_policy_assignment.ps1", map[string]string{
		"Action":     "Get",
		"GroupId":    prior.GroupID.ValueString(),
		"PolicyType": prior.PolicyType.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Failed to read group policy assignment", err.Error())
		return
	}
	if client.IsNotFound(output) {
		resp.State.RemoveResource(ctx)
		return
	}
	state, err := groupPolicyAssignmentFromAPI(prior, output)
	if err != nil {
		resp.Diagnostics.AddError("Failed to parse PowerShell response", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Update is never called: every attribute requires a replacement.
func (r *groupPolicyAssignmentResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unexpected update", "Group policy assignments cannot be changed in place.")
}

func (r *groupPolicyAssignmentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state groupPolicyAssignmentModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.RunScript(ctx, "group_policy_assignment.ps1", map[string]string{
		"Action":     "Delete",
		"GroupId":    state.GroupID.ValueString(),
		"PolicyType": state.PolicyType.ValueString(),
	}); err != nil {
		resp.Diagnostics.AddError("Failed to delete group policy assignment", err.Error())
	}
}

// ImportState accepts "<group_id>/<policy_type>".
func (r *groupPolicyAssignmentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	groupID, policyType, ok := strings.Cut(req.ID, "/")
	if !ok || groupID == "" || policyType == "" {
		resp.Diagnostics.AddError("Invalid import ID", fmt.Sprintf("Expected <group_id>/<policy_type>, got %q.", req.ID))
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("group_id"), groupID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("policy_type"), policyType)...)
}

// groupPolicyAssignmentFromAPI builds the state from the bridge response. prior
// keeps the configured case of the policy type and name.
func groupPolicyAssignmentFromAPI(prior groupPolicyAssignmentModel, output []byte) (groupPolicyAssignmentModel, error) {
	var a groupPolicyAssignmentAPIResponse
	if err := json.Unmarshal(output, &a); err != nil {
		return prior, err
	}
	groupID := prior.GroupID
	if a.GroupID != "" && !strings.EqualFold(a.GroupID, groupID.ValueString()) {
		groupID = types.StringValue(a.GroupID)
	}
	policyType := foldString(prior.PolicyType, a.PolicyType)
	return groupPolicyAssignmentModel{
		ID:         types.StringValue(groupID.ValueString() + "/" + policyType.ValueString()),
		GroupID:    groupID,
		PolicyType: policyType,
		PolicyName: foldString(prior.PolicyName, a.PolicyName),
		Rank:       types.Int64Value(a.Rank),
	}, nil
}
