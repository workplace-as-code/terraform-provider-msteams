package resources

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestCaseInsensitiveModifier(t *testing.T) {
	cases := []struct {
		name          string
		state, config types.String
		want          types.String
	}{
		{"case only: keep state", types.StringValue("Strict"), types.StringValue("STRICT"), types.StringValue("Strict")},
		{"real change: use config", types.StringValue("Strict"), types.StringValue("Moderate"), types.StringValue("Moderate")},
		{"no state (create): use config", types.StringNull(), types.StringValue("STRICT"), types.StringValue("STRICT")},
		{"unset in config: untouched", types.StringValue("Strict"), types.StringNull(), types.StringNull()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := planmodifier.StringRequest{StateValue: tc.state, ConfigValue: tc.config, PlanValue: tc.config}
			resp := &planmodifier.StringResponse{PlanValue: tc.config}
			caseInsensitive().PlanModifyString(context.Background(), req, resp)
			if !resp.PlanValue.Equal(tc.want) {
				t.Errorf("plan = %v, want %v", resp.PlanValue, tc.want)
			}
		})
	}
}

func TestFoldString(t *testing.T) {
	if got := foldString(types.StringValue("STRICT"), "Strict"); got.ValueString() != "STRICT" {
		t.Errorf("case-only difference: got %q, want the configured STRICT", got.ValueString())
	}
	if got := foldString(types.StringValue("STRICT"), "Moderate"); got.ValueString() != "Moderate" {
		t.Errorf("real difference: got %q, want Moderate", got.ValueString())
	}
	if got := foldString(types.StringNull(), "Strict"); got.ValueString() != "Strict" {
		t.Errorf("no prior value: got %q", got.ValueString())
	}
}
