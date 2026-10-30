package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dsschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
)

// TestDataSourcesMirrorTheirResource checks every data source built from a
// resource: a policy has name as its only argument, a configuration none, and
// the value type is the resource's, since Read copies the resource state as is.
func TestDataSourcesMirrorTheirResource(t *testing.T) {
	ctx := context.Background()
	if len(DataSources()) != len(objectSpecs) {
		t.Fatalf("%d data sources for %d generic resources", len(DataSources()), len(objectSpecs))
	}
	for _, newDataSource := range DataSources() {
		d := newDataSource().(*resourceDataSource)
		t.Run(d.typeName(ctx), func(t *testing.T) {
			var resp datasource.SchemaResponse
			d.Schema(ctx, datasource.SchemaRequest{}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatal(resp.Diagnostics)
			}
			for name, a := range resp.Schema.Attributes {
				isKey := d.key != "" && name == d.key
				if a.IsRequired() != isKey || (!isKey && !a.IsComputed()) {
					t.Errorf("%s: required=%v computed=%v", name, a.IsRequired(), a.IsComputed())
				}
			}
			if d.key != "" {
				if _, ok := resp.Schema.Attributes[d.key].(dsschema.StringAttribute); !ok {
					t.Errorf("no %s attribute", d.key)
				}
			}
			want := d.resourceSchema(ctx).Type().TerraformType(ctx)
			if got := resp.Schema.Type().TerraformType(ctx); !got.Equal(want) {
				t.Errorf("type differs from the resource:\n%s\n%s", got, want)
			}
		})
	}
}
