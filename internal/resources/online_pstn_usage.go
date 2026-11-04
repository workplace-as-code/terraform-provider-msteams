package resources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/workplace-as-code/terraform-provider-msteams/internal/client"
)

var (
	_ resource.Resource                = &onlinePstnUsageResource{}
	_ resource.ResourceWithImportState = &onlinePstnUsageResource{}
)

// onlinePstnUsageResource is one name in the tenant's list of PSTN usages
// (Set-CsOnlinePstnUsage -Usage @{Add/Remove}). The list is a single Teams
// object: a resource per usage lets each voice route and policy refer to the
// usages it needs, and Terraform order the changes.
type onlinePstnUsageResource struct {
	client *client.Client
}

type onlinePstnUsageModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

// NewOnlinePstnUsageResource is registered in Resources().
func NewOnlinePstnUsageResource() resource.Resource {
	return &onlinePstnUsageResource{}
}

func (r *onlinePstnUsageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_online_pstn_usage"
}

func (r *onlinePstnUsageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a PSTN usage of the tenant (`Set-CsOnlinePstnUsage`), a name that links Direct Routing voice routes " +
			"to the voice routing policies whose users can take them. Destroying the resource removes the usage from the tenant: " +
			"Teams refuses while a voice route or policy still uses it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The usage (same as `name`).",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The usage, for example `France` or `International`. Changing it replaces the usage.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
				PlanModifiers:       []planmodifier.String{caseInsensitive(), stringplanmodifier.RequiresReplace()},
			},
		},
	}
}

func (r *onlinePstnUsageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if c := clientFromProviderData(req, resp); c != nil {
		r.client = c
	}
}

// run calls online_pstn_usage.ps1 and returns the usage as Teams stores it,
// or "" when the tenant does not have it.
func (r *onlinePstnUsageResource) run(ctx context.Context, action, name string) (string, error) {
	output, err := r.client.RunScript(ctx, "online_pstn_usage.ps1", map[string]string{"Action": action, "Name": name})
	if err != nil || client.IsNotFound(output) {
		return "", err
	}
	var usage struct {
		Name string `json:"Name"`
	}
	if err := json.Unmarshal(output, &usage); err != nil {
		return "", fmt.Errorf("failed to parse PowerShell response: %w", err)
	}
	return usage.Name, nil
}

func (r *onlinePstnUsageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan onlinePstnUsageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name, err := r.run(ctx, "Create", plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to add the PSTN usage", err.Error())
		return
	}
	value := foldString(plan.Name, name)
	resp.Diagnostics.Append(resp.State.Set(ctx, onlinePstnUsageModel{ID: value, Name: value})...)
}

func (r *onlinePstnUsageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state onlinePstnUsageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name, err := r.run(ctx, "Get", state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read the PSTN usage", err.Error())
		return
	}
	if name == "" {
		resp.State.RemoveResource(ctx)
		return
	}
	value := foldString(state.Name, name)
	resp.Diagnostics.Append(resp.State.Set(ctx, onlinePstnUsageModel{ID: value, Name: value})...)
}

// Update is never called: the only argument replaces the usage.
func (r *onlinePstnUsageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan onlinePstnUsageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *onlinePstnUsageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state onlinePstnUsageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.run(ctx, "Delete", state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to remove the PSTN usage", err.Error())
	}
}

func (r *onlinePstnUsageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}
