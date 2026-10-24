package resources

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// TestEveryResourceIsListed checks that each resource has an identity, can be
// imported, and has a list resource of the same name that knows how to list it.
func TestEveryResourceIsListed(t *testing.T) {
	ctx := context.Background()
	names := map[string]bool{}
	for _, newResource := range Resources() {
		r := newResource()
		if _, ok := r.(resource.ResourceWithIdentity); !ok {
			t.Errorf("%T has no identity", r)
		}
		inner := r.(*identityResource).Resource
		if _, ok := inner.(resource.ResourceWithImportState); !ok {
			t.Errorf("%T cannot be imported", inner)
		}
		names[typeName(ctx, r.Metadata)] = true
	}
	for _, reg := range registrations() {
		if (reg.list.noun == "") == (reg.list.id == "") {
			t.Errorf("%s: a list query needs a noun or an id, not both", typeName(ctx, reg.newResource().Metadata))
		}
	}
	lists := ListResources()
	if len(lists) != len(names) {
		t.Fatalf("%d list resources for %d resources", len(lists), len(names))
	}
	for _, newList := range lists {
		if name := typeName(ctx, newList().Metadata); !names[name] {
			t.Errorf("list resource %s has no resource", name)
		}
	}
}

func typeName(ctx context.Context, metadata func(context.Context, resource.MetadataRequest, *resource.MetadataResponse)) string {
	var resp resource.MetadataResponse
	metadata(ctx, resource.MetadataRequest{ProviderTypeName: "msteams"}, &resp)
	return resp.TypeName
}

func TestIdentityFollowsTheIDAttribute(t *testing.T) {
	ctx := context.Background()
	r := &identityResource{Resource: &fakeResource{}}
	req := listRequest(t, r, true)

	result := req.NewListResult(ctx)
	state := stateOf(result)
	state.SetAttribute(ctx, path.Root("id"), "contoso")
	state.SetAttribute(ctx, path.Root("name"), "contoso")
	resp := resource.ReadResponse{State: state, Identity: result.Identity}
	r.Read(ctx, resource.ReadRequest{State: state}, &resp)
	var id types.String
	resp.Diagnostics.Append(resp.Identity.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() || id.ValueString() != "contoso" {
		t.Errorf("identity = %s, diags %v", id, resp.Diagnostics)
	}
}

func TestImportByIdentity(t *testing.T) {
	ctx := context.Background()
	r := &identityResource{Resource: &fakeResource{}}
	req := listRequest(t, r, true)
	identity := req.NewListResult(ctx).Identity
	identity.SetAttribute(ctx, path.Root("id"), "fabrikam")

	resp := resource.ImportStateResponse{State: stateOf(req.NewListResult(ctx)), Identity: identity}
	r.ImportState(ctx, resource.ImportStateRequest{Identity: identity}, &resp)
	var name types.String
	resp.Diagnostics.Append(resp.State.GetAttribute(ctx, path.Root("name"), &name)...)
	if resp.Diagnostics.HasError() || name.ValueString() != "fabrikam" {
		t.Errorf("name = %s, diags %v", name, resp.Diagnostics)
	}
}

func TestStreamReadsInOrderAndSkipsDeletedObjects(t *testing.T) {
	ctx := context.Background()
	fake := &fakeResource{}
	l := &listResource{newResource: func() resource.Resource { return fake }}
	req := listRequest(t, &identityResource{Resource: fake}, true)
	ids := []string{"a", "b", "gone", "c", "d", "e", "f"}

	var got []string
	l.stream(ctx, req, fake, ids)(func(result list.ListResult) bool {
		if result.Diagnostics.HasError() {
			t.Fatal(result.Diagnostics)
		}
		var name types.String
		result.Resource.GetAttribute(ctx, path.Root("name"), &name)
		got = append(got, result.DisplayName+"="+name.ValueString())
		return true
	})
	want := "a=A b=B c=C d=D e=E f=F"
	if strings.Join(got, " ") != want {
		t.Errorf("got %q, want %q", strings.Join(got, " "), want)
	}
}

func TestStreamStopsWhenTerraformHasEnough(t *testing.T) {
	fake := &fakeResource{}
	l := &listResource{newResource: func() resource.Resource { return fake }}
	req := listRequest(t, &identityResource{Resource: fake}, false)
	count := 0
	l.stream(context.Background(), req, fake, []string{"a", "b", "c"})(func(list.ListResult) bool {
		count++
		return count < 2
	})
	if count != 2 {
		t.Errorf("pushed %d results after Terraform stopped at 2", count)
	}
}

func TestSingletonIsListedWithoutCallingTeams(t *testing.T) {
	l := &listResource{query: listQuery{id: "Global"}}
	ids, err := l.ids(context.Background())
	if err != nil || len(ids) != 1 || ids[0] != "Global" {
		t.Errorf("ids = %v, %v", ids, err)
	}
}

// fakeResource imports an ID as its name and reads the name back in upper
// case. The object "gone" no longer exists.
type fakeResource struct{}

func (f *fakeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_fake"
}

func (f *fakeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Attributes: map[string]schema.Attribute{
		"id":   schema.StringAttribute{Computed: true},
		"name": schema.StringAttribute{Required: true},
	}}
}

func (f *fakeResource) Create(context.Context, resource.CreateRequest, *resource.CreateResponse) {}
func (f *fakeResource) Update(context.Context, resource.UpdateRequest, *resource.UpdateResponse) {}
func (f *fakeResource) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {}

func (f *fakeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if id.ValueString() == "gone" {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), strings.ToUpper(id.ValueString()))...)
}

func (f *fakeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), req.ID)...)
}

func stateOf(result list.ListResult) tfsdk.State {
	return tfsdk.State{Schema: result.Resource.Schema, Raw: result.Resource.Raw}
}

// listRequest builds the request Terraform sends to list r.
func listRequest(t *testing.T, r *identityResource, includeResource bool) list.ListRequest {
	t.Helper()
	ctx := context.Background()
	var s resource.SchemaResponse
	r.Schema(ctx, resource.SchemaRequest{}, &s)
	var i resource.IdentitySchemaResponse
	r.IdentitySchema(ctx, resource.IdentitySchemaRequest{}, &i)
	if s.Diagnostics.HasError() || i.Diagnostics.HasError() {
		t.Fatal(s.Diagnostics, i.Diagnostics)
	}
	return list.ListRequest{IncludeResource: includeResource, ResourceSchema: s.Schema, ResourceIdentitySchema: i.IdentitySchema}
}
