package resources

import (
	"context"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
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

func specByType(t *testing.T, typeName string) objectSpec {
	t.Helper()
	for _, s := range objectSpecs {
		if s.typeName == typeName {
			return s
		}
	}
	t.Fatalf("no spec %s", typeName)
	return objectSpec{}
}

func TestBlockListRoundTrip(t *testing.T) {
	spec := specByType(t, "tenant_dial_plan")
	var rules field
	for _, f := range spec.fields {
		if f.kind == blockListField {
			rules = f
		}
	}
	// PowerShell unwraps a one-element array: a single object is a list of one.
	list, diags := blockListValue(rules, map[string]any{"Name": "Ext", "Pattern": "^(\\d{4})$", "Translation": "+33$1",
		"Description": "", "IsInternalExtension": true})
	if diags.HasError() || len(list.Elements()) != 1 {
		t.Fatalf("got %v %v", list, diags)
	}
	sent := blockValues(rules, list)
	if len(sent) != 1 || sent[0]["Name"] != "Ext" || sent[0]["Translation"] != "+33$1" || sent[0]["IsInternalExtension"] != true {
		t.Errorf("sent %v", sent)
	}
	if empty, _ := blockListValue(rules, nil); len(empty.Elements()) != 0 {
		t.Errorf("no rules gives %v", empty)
	}
}

func TestValidateEmergencyCallingPolicy(t *testing.T) {
	check := func(mode, group attr.Value) bool {
		var diags diag.Diagnostics
		validateEmergencyCallingPolicy(map[string]attr.Value{"notification_mode": mode, "notification_group": group}, &diags)
		return diags.HasError()
	}
	if !check(types.StringValue("notificationonly"), types.StringNull()) {
		t.Error("NotificationOnly without a group must fail")
	}
	if check(types.StringValue("NotificationOnly"), types.StringValue("security@contoso.com")) {
		t.Error("NotificationOnly with a group is valid")
	}
	if check(types.StringValue("ConferenceMuted"), types.StringNull()) || check(types.StringUnknown(), types.StringNull()) {
		t.Error("other or unknown modes are not checked")
	}
}
