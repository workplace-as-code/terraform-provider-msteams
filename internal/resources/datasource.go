package resources

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// Data sources built from resources
//
// A data source reads an existing policy by name, or the tenant's single
// configuration, and exposes the attributes of its resource, all computed. It
// runs the resource's own ImportState and Read, like terraform query does:
// the data source always reads exactly what the resource reads, and a new
// attribute of the resource appears in both.

// providerTypeName prefixes the type names of the resources.
const providerTypeName = "msteams"

// dataSourceResource pairs a resource with the argument of its data source,
// whose value is the import ID. A configuration has no argument: there is a
// single one per tenant, Global.
type dataSourceResource struct {
	newResource func() resource.Resource
	key         string
}

// dataSourceResources returns every generic resource: policies are read by
// name, configurations without an argument. Group policy assignments have no
// data source: they are identified by a group and a policy type, not a name.
func dataSourceResources() []dataSourceResource {
	out := make([]dataSourceResource, 0, len(objectSpecs))
	for _, spec := range objectSpecs {
		key := "name"
		if spec.singleton {
			key = ""
		}
		out = append(out, dataSourceResource{newObjectResource(spec), key})
	}
	return out
}

// DataSources returns the data sources built from resources.
func DataSources() []func() datasource.DataSource {
	sources := dataSourceResources()
	out := make([]func() datasource.DataSource, 0, len(sources))
	for _, source := range sources {
		out = append(out, func() datasource.DataSource {
			return &resourceDataSource{resource: source.newResource(), key: source.key}
		})
	}
	return out
}

// resourceDataSource is a data source that reads through a resource.
type resourceDataSource struct {
	resource resource.Resource
	key      string
}

var _ datasource.DataSourceWithConfigure = &resourceDataSource{}

func (d *resourceDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	var meta resource.MetadataResponse
	d.resource.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: req.ProviderTypeName}, &meta)
	resp.TypeName = meta.TypeName
}

// resourceSchema returns the schema of the resource behind the data source.
func (d *resourceDataSource) resourceSchema(ctx context.Context) schema.Schema {
	var resp resource.SchemaResponse
	d.resource.Schema(ctx, resource.SchemaRequest{}, &resp)
	return resp.Schema
}

// typeName returns the type name of the data source, e.g. msteams_meeting_policy.
func (d *resourceDataSource) typeName(ctx context.Context) string {
	var meta datasource.MetadataResponse
	d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: providerTypeName}, &meta)
	return meta.TypeName
}

func (d *resourceDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	typeName := d.typeName(ctx)
	rs := d.resourceSchema(ctx)
	attrs := make(map[string]dsschema.Attribute, len(rs.Attributes))
	for name, a := range rs.Attributes {
		converted, err := dataSourceAttribute(d.key != "" && name == d.key, a)
		if err != nil {
			resp.Diagnostics.AddError("Invalid data source schema", fmt.Sprintf("attribute %q: %s", name, err))
			return
		}
		attrs[name] = converted
	}
	link := fmt.Sprintf("[`%s`](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/resources/%s)",
		typeName, typeName[len(providerTypeName+"_"):])
	desc := fmt.Sprintf("Reads an existing policy by `name`, with the attributes of the %s resource. "+
		"Use it to reference a policy that this configuration does not manage, such as `Global`.", link)
	if d.key == "" {
		desc = fmt.Sprintf("Reads the tenant's configuration (`Global`), with the attributes of the %s resource, "+
			"without managing it.", link)
	}
	resp.Schema = dsschema.Schema{MarkdownDescription: desc, Attributes: attrs}
}

// dataSourceAttribute turns a resource attribute into a computed data source
// attribute; the key, which names the policy to read, is the only argument.
func dataSourceAttribute(key bool, a schema.Attribute) (dsschema.Attribute, error) {
	desc := a.GetMarkdownDescription()
	if key {
		desc = "Name of the policy to read. `Global` reads the org-wide default policy."
	}
	switch a := a.(type) {
	case schema.StringAttribute:
		return dsschema.StringAttribute{Required: key, Computed: !key, Sensitive: a.Sensitive, MarkdownDescription: desc}, nil
	case schema.BoolAttribute:
		return dsschema.BoolAttribute{Computed: true, Sensitive: a.Sensitive, MarkdownDescription: desc}, nil
	case schema.Int64Attribute:
		return dsschema.Int64Attribute{Computed: true, Sensitive: a.Sensitive, MarkdownDescription: desc}, nil
	case schema.ListAttribute:
		return dsschema.ListAttribute{Computed: true, ElementType: a.ElementType, Sensitive: a.Sensitive, MarkdownDescription: desc}, nil
	case schema.SetAttribute:
		return dsschema.SetAttribute{Computed: true, ElementType: a.ElementType, Sensitive: a.Sensitive, MarkdownDescription: desc}, nil
	}
	return nil, fmt.Errorf("%T has no data source equivalent", a)
}

func (d *resourceDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	configurable, ok := d.resource.(resource.ResourceWithConfigure)
	if !ok {
		return
	}
	var configured resource.ConfigureResponse
	configurable.Configure(ctx, resource.ConfigureRequest{ProviderData: req.ProviderData}, &configured)
	resp.Diagnostics.Append(configured.Diagnostics...)
}

func (d *resourceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	importer, ok := d.resource.(resource.ResourceWithImportState)
	if !ok {
		resp.Diagnostics.AddError("Data source not supported", "This resource cannot be imported, so it cannot be read.")
		return
	}
	// A configuration ignores the import ID: it is always Global.
	id := "Global"
	if d.key != "" {
		var name types.String
		resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root(d.key), &name)...)
		if resp.Diagnostics.HasError() {
			return
		}
		id = name.ValueString()
	}

	// The same steps as an import: an empty state with the import ID, then Read.
	rs := d.resourceSchema(ctx)
	empty := tfsdk.State{Schema: rs, Raw: tftypes.NewValue(rs.Type().TerraformType(ctx), nil)}
	imported := resource.ImportStateResponse{State: empty}
	importer.ImportState(ctx, resource.ImportStateRequest{ID: id}, &imported)
	resp.Diagnostics.Append(imported.Diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	read := resource.ReadResponse{State: imported.State}
	d.resource.Read(ctx, resource.ReadRequest{State: imported.State}, &read)
	resp.Diagnostics.Append(read.Diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	if read.State.Raw.IsNull() {
		resp.Diagnostics.AddError("Object not found", fmt.Sprintf("No %s named %q exists in Microsoft Teams.", d.typeName(ctx), id))
		return
	}
	// Both schemas have the same attributes and types, so the value fits as is.
	resp.State.Raw = read.State.Raw
}
