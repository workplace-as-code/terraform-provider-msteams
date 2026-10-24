package resources

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/workplace-as-code/terraform-provider-msteams/internal/client"
)

// clientFromProviderData extracts the shared client in each resource's Configure.
// ProviderData is nil during early validation, before the provider is configured.
func clientFromProviderData(req resource.ConfigureRequest, resp *resource.ConfigureResponse) *client.Client {
	if req.ProviderData == nil {
		return nil
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data type", fmt.Sprintf("Expected *client.Client, got %T", req.ProviderData))
		return nil
	}
	return c
}

// foldString returns the configured value when Teams returns the same value
// with a different case (e.g. "STRICT" vs "Strict"), so case alone never shows
// up as drift.
func foldString(prior types.String, api string) types.String {
	if !prior.IsNull() && !prior.IsUnknown() && strings.EqualFold(prior.ValueString(), api) {
		return prior
	}
	return types.StringValue(api)
}

// optionalBool is an attribute that Terraform only manages when it is set in
// the configuration. When it is omitted, the current Teams value is kept and
// recorded in state.
func optionalBool(description string) schema.BoolAttribute {
	return schema.BoolAttribute{
		Optional:            true,
		Computed:            true,
		MarkdownDescription: description,
		PlanModifiers:       []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
	}
}

func optionalString(description string, validators ...validator.String) schema.StringAttribute {
	return schema.StringAttribute{
		Optional:            true,
		Computed:            true,
		MarkdownDescription: description,
		Validators:          validators,
		PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
	}
}

func optionalList(description string) schema.ListAttribute {
	return schema.ListAttribute{
		ElementType:         types.StringType,
		Optional:            true,
		Computed:            true,
		MarkdownDescription: description,
		PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
	}
}

// listValue converts the strings returned by Teams to a list attribute.
func listValue(values []string) types.List {
	elems := make([]attr.Value, 0, len(values))
	for _, v := range values {
		elems = append(elems, types.StringValue(v))
	}
	return types.ListValueMust(types.StringType, elems)
}

func optionalInt64(description string, validators ...validator.Int64) schema.Int64Attribute {
	return schema.Int64Attribute{
		Optional:            true,
		Computed:            true,
		MarkdownDescription: description,
		Validators:          validators,
		PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
	}
}

// optionalEnum is an optionalString restricted to a set of values, compared
// without regard to case. Teams accepts "STRICT" and returns "Strict"; the plan
// modifier and foldString make sure that never shows up as a change.
func optionalEnum(description string, values ...string) schema.StringAttribute {
	return schema.StringAttribute{
		Optional:            true,
		Computed:            true,
		MarkdownDescription: description,
		Validators:          []validator.String{stringvalidator.OneOfCaseInsensitive(values...)},
		PlanModifiers: []planmodifier.String{
			stringplanmodifier.UseStateForUnknown(),
			caseInsensitive(),
		},
	}
}

// caseInsensitive keeps the value from state when the configuration only
// differs from it by case, for example after importing an object whose value
// Teams returns as "Strict" while the configuration says "STRICT".
func caseInsensitive() planmodifier.String {
	return caseInsensitiveModifier{}
}

type caseInsensitiveModifier struct{}

func (caseInsensitiveModifier) Description(context.Context) string {
	return "Ignores differences in case between the configuration and the current value."
}

func (m caseInsensitiveModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (caseInsensitiveModifier) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() || req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if req.StateValue.ValueString() != req.ConfigValue.ValueString() && strings.EqualFold(req.StateValue.ValueString(), req.ConfigValue.ValueString()) {
		resp.PlanValue = req.StateValue
	}
}
