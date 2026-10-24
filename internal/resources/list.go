package resources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/list"
	listschema "github.com/hashicorp/terraform-plugin-framework/list/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/identityschema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/workplace-as-code/terraform-provider-msteams/internal/client"
)

// Every resource has a resource identity holding its import ID, and a list
// resource of the same name that enumerates the existing objects, so that
// `terraform query` can find them and generate their configuration
// (Terraform 1.14 and later).
//
// Both are added around the resources rather than in each of them: every
// resource already sets its id attribute to its import ID, and listing an
// object with its data is what `terraform import` does with that ID:
// ImportState, then Read.

// listConcurrency is how many objects are read at once while listing. The
// client still caps the number of PowerShell processes (TEAMS_MAX_CONCURRENCY).
const listConcurrency = 4

// identityResource adds the resource identity to a resource.
type identityResource struct {
	resource.Resource
}

var (
	_ resource.ResourceWithConfigure      = &identityResource{}
	_ resource.ResourceWithIdentity       = &identityResource{}
	_ resource.ResourceWithImportState    = &identityResource{}
	_ resource.ResourceWithModifyPlan     = &identityResource{}
	_ resource.ResourceWithValidateConfig = &identityResource{}
)

func withIdentity(newResource func() resource.Resource) func() resource.Resource {
	return func() resource.Resource { return &identityResource{Resource: newResource()} }
}

func (r *identityResource) IdentitySchema(_ context.Context, _ resource.IdentitySchemaRequest, resp *resource.IdentitySchemaResponse) {
	resp.IdentitySchema = identityschema.Schema{
		Attributes: map[string]identityschema.Attribute{
			"id": identityschema.StringAttribute{
				RequiredForImport: true,
				Description:       "The import ID of the object, the same value as its `id` attribute.",
			},
		},
	}
}

func (r *identityResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if c, ok := r.Resource.(resource.ResourceWithConfigure); ok {
		c.Configure(ctx, req, resp)
	}
}

func (r *identityResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	if v, ok := r.Resource.(resource.ResourceWithValidateConfig); ok {
		v.ValidateConfig(ctx, req, resp)
	}
}

func (r *identityResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if m, ok := r.Resource.(resource.ResourceWithModifyPlan); ok {
		m.ModifyPlan(ctx, req, resp)
	}
}

func (r *identityResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.Resource.Create(ctx, req, resp)
	setIdentity(ctx, resp.State, resp.Identity, &resp.Diagnostics)
}

func (r *identityResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	r.Resource.Read(ctx, req, resp)
	if resp.State.Raw.IsNull() {
		// The object no longer exists. The framework still requires an
		// identity after Read, even when importing by ID an object that does
		// not exist: keep the one of the requested ID, so that Terraform
		// reports a missing object instead of a missing identity.
		setIdentity(ctx, req.State, resp.Identity, &resp.Diagnostics)
		return
	}
	setIdentity(ctx, resp.State, resp.Identity, &resp.Diagnostics)
}

func (r *identityResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.Resource.Update(ctx, req, resp)
	setIdentity(ctx, resp.State, resp.Identity, &resp.Diagnostics)
}

// ImportState accepts the import ID or, from an import block, the identity.
func (r *identityResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == "" && req.Identity != nil {
		var id types.String
		resp.Diagnostics.Append(req.Identity.GetAttribute(ctx, path.Root("id"), &id)...)
		if resp.Diagnostics.HasError() {
			return
		}
		req.ID = id.ValueString()
	}
	importer, ok := r.Resource.(resource.ResourceWithImportState)
	if !ok {
		resp.Diagnostics.AddError("Import not supported", "This resource cannot be imported.")
		return
	}
	importer.ImportState(ctx, req, resp)
}

// setIdentity copies the id attribute of a state to the identity. A null state
// leaves the identity unchanged.
func setIdentity(ctx context.Context, state tfsdk.State, identity *tfsdk.ResourceIdentity, diags *diag.Diagnostics) {
	if diags.HasError() || identity == nil || state.Raw.IsNull() {
		return
	}
	var id types.String
	diags.Append(state.GetAttribute(ctx, path.Root("id"), &id)...)
	if !diags.HasError() {
		diags.Append(identity.SetAttribute(ctx, path.Root("id"), id)...)
	}
}

// listQuery says how list.ps1 enumerates the import IDs of a resource type.
type listQuery struct {
	noun string // Get-<noun> returns the objects
	id   string // configuration: its only import ID, nothing to call
}

// listResource lists the objects of one resource type.
type listResource struct {
	newResource func() resource.Resource // the resource, without identity
	query       listQuery
	client      *client.Client
}

var _ list.ListResourceWithConfigure = &listResource{}

func newListResource(newResource func() resource.Resource, query listQuery) func() list.ListResource {
	return func() list.ListResource { return &listResource{newResource: newResource, query: query} }
}

func (l *listResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	l.newResource().Metadata(ctx, req, resp)
}

func (l *listResource) ListResourceConfigSchema(_ context.Context, _ list.ListResourceSchemaRequest, resp *list.ListResourceSchemaResponse) {
	resp.Schema = listschema.Schema{
		MarkdownDescription: "Lists the existing objects of this type, built-in ones included, for `terraform query` (Terraform 1.14 and later). " +
			"The list block takes no arguments.",
	}
}

func (l *listResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if c := clientFromProviderData(req, resp); c != nil {
		l.client = c
	}
}

func (l *listResource) List(ctx context.Context, req list.ListRequest, stream *list.ListResultsStream) {
	ids, err := l.ids(ctx)
	if err != nil {
		var diags diag.Diagnostics
		diags.AddError("Failed to list objects", err.Error())
		stream.Results = list.ListResultsStreamDiagnostics(diags)
		return
	}
	if req.Limit > 0 && int64(len(ids)) > req.Limit {
		ids = ids[:req.Limit]
	}
	r := l.newResource()
	if c, ok := r.(resource.ResourceWithConfigure); ok {
		c.Configure(ctx, resource.ConfigureRequest{ProviderData: l.client}, &resource.ConfigureResponse{})
	}

	stream.Results = l.stream(ctx, req, r, ids)
}

// stream reads the objects in parallel and pushes them in the listed order.
// It stops at the first error.
func (l *listResource) stream(ctx context.Context, req list.ListRequest, r resource.Resource, ids []string) func(func(list.ListResult) bool) {
	return func(push func(list.ListResult) bool) {
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()

		type item struct {
			result list.ListResult
			found  bool
		}
		items := make([]chan item, len(ids))
		for i := range items {
			items[i] = make(chan item, 1)
		}
		go func() {
			slots := make(chan struct{}, listConcurrency)
			for i, id := range ids {
				select {
				case slots <- struct{}{}:
				case <-ctx.Done():
					return
				}
				go func() {
					defer func() { <-slots }()
					result, found := l.result(ctx, req, r, id)
					items[i] <- item{result, found}
				}()
			}
		}()

		for _, ch := range items {
			var it item
			select {
			case it = <-ch:
			case <-ctx.Done():
				return
			}
			if it.found && !push(it.result) {
				return
			}
			if it.result.Diagnostics.HasError() {
				return
			}
		}
	}
}

// ids returns the import IDs of the existing objects.
func (l *listResource) ids(ctx context.Context) ([]string, error) {
	if l.query.id != "" {
		return []string{l.query.id}, nil
	}
	if l.client == nil {
		return nil, fmt.Errorf("the provider is not configured")
	}
	output, err := l.client.RunScript(ctx, "list.ps1", map[string]string{"Noun": l.query.noun})
	if err != nil {
		return nil, err
	}
	var ids []string
	if err := json.Unmarshal(output, &ids); err != nil {
		return nil, fmt.Errorf("failed to parse the list of %s objects: %w", l.query.noun, err)
	}
	return ids, nil
}

// result builds the list result of one object. When Terraform asks for the
// object data, it is imported and read as `terraform import` would; found is
// false when the object was deleted in the meantime.
func (l *listResource) result(ctx context.Context, req list.ListRequest, r resource.Resource, id string) (result list.ListResult, found bool) {
	result = req.NewListResult(ctx)
	result.DisplayName = id
	if req.IncludeResource {
		importer, ok := r.(resource.ResourceWithImportState)
		if !ok {
			result.Diagnostics.AddError("Import not supported", "This resource cannot be imported.")
			return result, true
		}
		imported := resource.ImportStateResponse{State: tfsdk.State{Schema: req.ResourceSchema, Raw: result.Resource.Raw}}
		importer.ImportState(ctx, resource.ImportStateRequest{ID: id}, &imported)
		result.Diagnostics.Append(imported.Diagnostics...)
		if result.Diagnostics.HasError() {
			return result, true
		}
		read := resource.ReadResponse{State: imported.State}
		r.Read(ctx, resource.ReadRequest{State: imported.State}, &read)
		result.Diagnostics.Append(read.Diagnostics...)
		if result.Diagnostics.HasError() {
			return result, true
		}
		if read.State.Raw.IsNull() {
			return result, false
		}
		result.Resource.Raw = read.State.Raw
		// As after an import, the identity is the id attribute the read set.
		var stateID types.String
		result.Diagnostics.Append(read.State.GetAttribute(ctx, path.Root("id"), &stateID)...)
		id = stateID.ValueString()
	}
	result.Diagnostics.Append(result.Identity.SetAttribute(ctx, path.Root("id"), id)...)
	return result, true
}
