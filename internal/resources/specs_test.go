package resources

import (
	"context"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var snakeCase = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// TestObjectSpecs checks every generic resource: unique snake_case attributes,
// unique cmdlet parameters, and a schema the framework accepts.
func TestObjectSpecs(t *testing.T) {
	seen := map[string]bool{}
	for _, spec := range objectSpecs {
		if seen[spec.typeName] {
			t.Errorf("duplicate resource %s", spec.typeName)
		}
		seen[spec.typeName] = true

		names, params := map[string]bool{"id": true, "name": true}, map[string]bool{}
		for _, f := range spec.fields {
			if !snakeCase.MatchString(f.name) || names[f.name] {
				t.Errorf("%s: invalid or duplicate attribute %q", spec.typeName, f.name)
			}
			if params[f.param] {
				t.Errorf("%s: parameter %s mapped twice", spec.typeName, f.param)
			}
			if f.kind == enumField && len(f.values) == 0 {
				t.Errorf("%s.%s: enum without values", spec.typeName, f.name)
			}
			names[f.name], params[f.param] = true, true
		}

		r := newObjectResource(spec)()
		var resp resource.SchemaResponse
		r.Schema(context.Background(), resource.SchemaRequest{}, &resp)
		resp.Diagnostics.Append(resp.Schema.ValidateImplementation(context.Background())...)
		if resp.Diagnostics.HasError() {
			t.Errorf("%s: invalid schema: %v", spec.typeName, resp.Diagnostics)
		}
	}
}

func TestFromAPIKeepsConfiguredCase(t *testing.T) {
	r := &objectResource{spec: objectSpecs[0]} // messaging_policy
	plan, diags := r.fromAPI(mustObject(t, r, map[string]string{"name": "Students", "giphy_rating_type": "STRICT"}),
		[]byte(`{"Name":"students","GiphyRatingType":"Strict","AllowGiphy":true}`))
	if diags.HasError() {
		t.Fatal(diags)
	}
	attrs := plan.Attributes()
	if got := attrs["name"].String(); got != `"Students"` {
		t.Errorf("name = %s, want the configured case", got)
	}
	if got := attrs["giphy_rating_type"].String(); got != `"STRICT"` {
		t.Errorf("giphy_rating_type = %s, want the configured case", got)
	}
	if got := attrs["allow_giphy"].String(); got != "true" {
		t.Errorf("allow_giphy = %s", got)
	}
}

// mustObject builds a plan object for r with the given string attributes set
// and every other attribute null.
func mustObject(t *testing.T, r *objectResource, values map[string]string) types.Object {
	t.Helper()
	attrs := map[string]attr.Value{}
	for name, typ := range r.spec.attrTypes() {
		if v, ok := values[name]; ok {
			attrs[name] = types.StringValue(v)
			continue
		}
		switch typ {
		case types.BoolType:
			attrs[name] = types.BoolNull()
		case types.Int64Type:
			attrs[name] = types.Int64Null()
		case types.StringType:
			attrs[name] = types.StringNull()
		default:
			attrs[name] = types.ListNull(types.StringType)
		}
	}
	obj, diags := types.ObjectValue(r.spec.attrTypes(), attrs)
	if diags.HasError() {
		t.Fatal(diags)
	}
	return obj
}

func TestToStrings(t *testing.T) {
	cases := map[string]struct {
		raw  any
		want int
	}{
		"null":            {nil, 0},
		"empty string":    {"", 0},
		"unwrapped value": {"contoso.com", 1},
		"array":           {[]any{"a", "b"}, 2},
	}
	for name, tc := range cases {
		if got := toStrings(tc.raw); len(got) != tc.want {
			t.Errorf("%s: toStrings(%v) = %v, want %d elements", name, tc.raw, got, tc.want)
		}
	}
}
