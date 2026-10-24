package resources

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/workplace-as-code/terraform-provider-msteams/internal/client"
)

// Teams settings follow two shapes, each handled by one generic resource:
//
//   - Policies (CsTeamsMeetingPolicy, CsTeamsCallingPolicy, ...): named objects
//     with New/Get/Set/Remove cmdlets and an "Identity". The built-in "Global"
//     policy (the org-wide default) can also be managed: it is adopted on create
//     and only removed from state on destroy.
//   - Configurations (CsTeamsClientConfiguration, ...): one "Global" object per
//     tenant with Get/Set cmdlets only.
//
// A resource is described by an objectSpec: its cmdlet noun and a list of
// fields, each mapping a Terraform attribute to a cmdlet parameter. The bridge
// script (cs_object.ps1 by default) receives the values as JSON and calls
// <verb>-<noun>, so adding a policy type only takes a spec.
//
// Every field is Optional + Computed: an attribute left out of the
// configuration keeps its current Teams value and is recorded in state.

type fieldKind int

const (
	boolField fieldKind = iota
	stringField
	int64Field
	enumField
	listField
)

// field maps one Terraform attribute to one cmdlet parameter and property.
type field struct {
	name       string // Terraform attribute, snake_case
	param      string // cmdlet parameter and object property
	kind       fieldKind
	desc       string
	values     []string // enumField only
	validators []validator.Int64
}

func boolAttr(name, param, desc string) field {
	return field{name: name, param: param, kind: boolField, desc: desc}
}

func stringAttr(name, param, desc string) field {
	return field{name: name, param: param, kind: stringField, desc: desc}
}

func int64Attr(name, param, desc string, validators ...validator.Int64) field {
	return field{name: name, param: param, kind: int64Field, desc: desc, validators: validators}
}

func listAttr(name, param, desc string) field {
	return field{name: name, param: param, kind: listField, desc: desc}
}

// enumAttr is a string restricted to values, compared without regard to case.
// The valid values are appended to the description.
func enumAttr(name, param, desc string, values ...string) field {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = "`" + v + "`"
	}
	desc = fmt.Sprintf("%s Valid values: %s (case-insensitive).", desc, strings.Join(quoted, ", "))
	return field{name: name, param: param, kind: enumField, desc: desc, values: values}
}

// objectSpec describes one Teams policy or configuration resource.
type objectSpec struct {
	typeName    string // resource name without the provider prefix
	noun        string // cmdlet noun, e.g. "CsTeamsMeetingPolicy"
	script      string // bridge script; "" means cs_object.ps1
	singleton   bool   // configuration with a single "Global" object
	description string // first sentence of the resource documentation
	fields      []field
}

// listQuery lists the policies; a configuration has the single ID "Global".
func (s objectSpec) listQuery() listQuery {
	if s.singleton {
		return listQuery{id: "Global"}
	}
	return listQuery{noun: s.noun}
}

func (s objectSpec) scriptName() string {
	if s.script != "" {
		return s.script
	}
	return "cs_object.ps1"
}

func (s objectSpec) attrTypes() map[string]attr.Type {
	types_ := map[string]attr.Type{"id": types.StringType}
	if !s.singleton {
		types_["name"] = types.StringType
	}
	for _, f := range s.fields {
		types_[f.name] = f.attrType()
	}
	return types_
}

func (f field) attrType() attr.Type {
	switch f.kind {
	case boolField:
		return types.BoolType
	case int64Field:
		return types.Int64Type
	case listField:
		return types.ListType{ElemType: types.StringType}
	default:
		return types.StringType
	}
}

func (f field) schemaAttribute() schema.Attribute {
	switch f.kind {
	case boolField:
		return optionalBool(f.desc)
	case int64Field:
		return optionalInt64(f.desc, f.validators...)
	case enumField:
		return optionalEnum(f.desc, f.values...)
	case listField:
		return optionalList(f.desc)
	default:
		return optionalString(f.desc)
	}
}

// objectResource is the generic resource built from an objectSpec.
type objectResource struct {
	spec   objectSpec
	client *client.Client
}

var (
	_ resource.Resource                = &objectResource{}
	_ resource.ResourceWithImportState = &objectResource{}
)

// newObjectResource returns the factory registered in the provider.
func newObjectResource(spec objectSpec) func() resource.Resource {
	return func() resource.Resource { return &objectResource{spec: spec} }
}

func (r *objectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.spec.typeName
}

func (r *objectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := map[string]schema.Attribute{}
	var desc string
	if r.spec.singleton {
		attrs["id"] = schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "Always `Global`.",
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		}
		desc = fmt.Sprintf("%s (`%s`). This is a singleton: there is exactly one configuration per tenant. "+
			"Only the attributes set in the configuration are managed; the others keep their current value. "+
			"Destroying this resource removes it from Terraform state without changing Teams.", r.spec.description, r.spec.noun)
	} else {
		attrs["id"] = schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: "The policy name (same as `name`).",
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		}
		attrs["name"] = schema.StringAttribute{
			Required: true,
			MarkdownDescription: "The policy name. Changing it creates a new policy. `Global` manages the org-wide default policy: " +
				"it is adopted instead of created, and destroying the resource only removes it from Terraform state.",
			Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			// Teams policy names are case-insensitive: a case-only change is not a new policy.
			PlanModifiers: []planmodifier.String{caseInsensitive(), stringplanmodifier.RequiresReplace()},
		}
		desc = fmt.Sprintf("%s (`%s`). Attributes left out of the configuration take the Teams default when the policy "+
			"is created and are not managed afterwards.", r.spec.description, r.spec.noun)
	}
	for _, f := range r.spec.fields {
		attrs[f.name] = f.schemaAttribute()
	}
	resp.Schema = schema.Schema{MarkdownDescription: desc, Attributes: attrs}
}

func (r *objectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if c := clientFromProviderData(req, resp); c != nil {
		r.client = c
	}
}

func (r *objectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan, config types.Object
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	action := "Create"
	if r.spec.singleton {
		action = "Update"
	}
	state, diags := r.write(ctx, action, plan, config, r.identity(plan))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *objectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var prior types.Object
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	output, err := r.client.RunScript(ctx, r.spec.scriptName(), r.params("Get", r.identity(prior), nil))
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Failed to read %s", r.spec.typeName), err.Error())
		return
	}
	if client.IsNotFound(output) {
		resp.State.RemoveResource(ctx)
		return
	}
	state, diags := r.fromAPI(prior, output)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *objectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, config, prior types.Object
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	state, diags := r.write(ctx, "Update", plan, config, r.identity(prior))
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

// Delete removes a custom policy. Configurations and the Global policy cannot
// be deleted: they are only removed from Terraform state.
func (r *objectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var prior types.Object
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}
	identity := r.identity(prior)
	if r.spec.singleton {
		warnStateOnlyDestroy(&resp.Diagnostics, r.spec.typeName, "Tenant-wide settings cannot be deleted.")
		return
	}
	if strings.EqualFold(identity, "Global") {
		warnStateOnlyDestroy(&resp.Diagnostics, r.spec.typeName, "The Global policy cannot be deleted.")
		return
	}
	if _, err := r.client.RunScript(ctx, r.spec.scriptName(), r.params("Delete", identity, nil)); err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Failed to delete %s", r.spec.typeName), err.Error())
	}
}

// ImportState accepts the policy name, with or without the "Tag:" prefix that
// Teams shows in Get-Cs*Policy output. Configurations are imported as "Global".
func (r *objectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if r.spec.singleton {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), "Global")...)
		return
	}
	name := strings.TrimPrefix(req.ID, "Tag:")
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), name)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
}

// identity returns the Teams identity of the object described by a plan or state.
func (r *objectResource) identity(o types.Object) string {
	if r.spec.singleton {
		return "Global"
	}
	if id, ok := o.Attributes()["id"].(types.String); ok && !id.IsNull() && !id.IsUnknown() && id.ValueString() != "" {
		return id.ValueString()
	}
	if name, ok := o.Attributes()["name"].(types.String); ok {
		return name.ValueString()
	}
	return ""
}

// params builds the bridge parameters. values holds the cmdlet parameters to
// set; Properties lists every property the script must return.
func (r *objectResource) params(action, identity string, values map[string]any) map[string]string {
	props := make([]string, 0, len(r.spec.fields))
	for _, f := range r.spec.fields {
		props = append(props, f.param)
	}
	p := map[string]string{
		"Action":     action,
		"Noun":       r.spec.noun,
		"Identity":   identity,
		"Properties": encodeJSON(props),
	}
	if values != nil {
		p["Values"] = encodeJSON(values)
	}
	return p
}

// write sends the known plan values to Teams and returns the resulting state.
// Only the attributes set in the configuration are sent: the plan also holds
// the current value of the others (UseStateForUnknown), and writing them back
// could fail (an empty address) or overwrite a change made in the meantime.
func (r *objectResource) write(ctx context.Context, action string, plan, config types.Object, identity string) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	values := map[string]any{}
	attrs, configured := plan.Attributes(), config.Attributes()
	for _, f := range r.spec.fields {
		v := attrs[f.name]
		if c := configured[f.name]; c == nil || c.IsNull() {
			continue // not in the configuration: not managed
		}
		if v == nil || v.IsNull() || v.IsUnknown() {
			continue // not configured: Teams keeps its current value
		}
		switch tv := v.(type) {
		case types.Bool:
			values[f.param] = tv.ValueBool()
		case types.Int64:
			values[f.param] = tv.ValueInt64()
		case types.String:
			values[f.param] = tv.ValueString()
		case types.List:
			items := []string{}
			diags.Append(tv.ElementsAs(ctx, &items, false)...)
			values[f.param] = items
		}
	}
	if diags.HasError() {
		return plan, diags
	}
	output, err := r.client.RunScript(ctx, r.spec.scriptName(), r.params(action, identity, values))
	if err != nil {
		diags.AddError(fmt.Sprintf("Failed to write %s", r.spec.typeName), err.Error())
		return plan, diags
	}
	state, d := r.fromAPI(plan, output)
	diags.Append(d...)
	return state, diags
}

// fromAPI builds the state from the JSON object returned by the bridge. prior
// is the plan or previous state: it supplies the name and keeps the configured
// case of enum values.
func (r *objectResource) fromAPI(prior types.Object, output []byte) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics
	var api map[string]any
	if err := json.Unmarshal(output, &api); err != nil {
		diags.AddError("Failed to parse PowerShell response", err.Error())
		return prior, diags
	}
	priorAttrs := prior.Attributes()
	values := map[string]attr.Value{}

	if r.spec.singleton {
		values["id"] = types.StringValue("Global")
	} else {
		priorName, _ := priorAttrs["name"].(types.String)
		name := priorName
		if apiName, _ := api["Name"].(string); apiName != "" {
			name = foldString(priorName, apiName)
		}
		values["id"] = name
		values["name"] = name
	}

	for _, f := range r.spec.fields {
		raw := api[f.param]
		switch f.kind {
		case boolField:
			b, _ := raw.(bool)
			values[f.name] = types.BoolValue(b)
		case int64Field:
			n, _ := raw.(float64)
			values[f.name] = types.Int64Value(int64(n))
		case listField:
			values[f.name] = listValue(toStrings(raw))
		case enumField:
			s, _ := raw.(string)
			priorValue, _ := priorAttrs[f.name].(types.String)
			values[f.name] = foldString(priorValue, s)
		default:
			s, _ := raw.(string)
			values[f.name] = types.StringValue(s)
		}
	}
	obj, d := types.ObjectValue(r.spec.attrTypes(), values)
	diags.Append(d...)
	return obj, diags
}

// toStrings converts a JSON array (or a single value) to strings. PowerShell
// can unwrap a one-element array to its value and an empty array to "", so
// both are accepted.
func toStrings(raw any) []string {
	switch v := raw.(type) {
	case nil:
		return []string{}
	case string:
		if v == "" {
			return []string{}
		}
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, fmt.Sprint(item))
		}
		return out
	default:
		return []string{fmt.Sprint(v)}
	}
}

// encodeJSON encodes a value as base64(JSON) for the bridge, so it survives
// the trip through the PowerShell host unchanged.
func encodeJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		// Only maps, slices and scalars are encoded here: this cannot fail.
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// warnStateOnlyDestroy tells the user that destroying the resource removed it
// from the Terraform state only: the object and its settings stay as they are.
func warnStateOnlyDestroy(diags *diag.Diagnostics, typeName, reason string) {
	diags.AddWarning(
		fmt.Sprintf("msteams_%s was only removed from the Terraform state", typeName),
		reason+" Destroying the resource only removes it from the Terraform state: its settings keep their current values. To undo a change made with Terraform, set the previous value and apply before destroying.",
	)
}
