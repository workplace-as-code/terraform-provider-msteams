package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestEnvOrConfigPrefersExplicitConfig(t *testing.T) {
	t.Setenv("TEAMS_TENANT_ID", "from-env.onmicrosoft.com")

	if got := envOrConfig("TEAMS_TENANT_ID", types.StringValue("from-hcl.onmicrosoft.com")); got != "from-hcl.onmicrosoft.com" {
		t.Errorf("provider block must win over the environment, got %q", got)
	}
	if got := envOrConfig("TEAMS_TENANT_ID", types.StringNull()); got != "from-env.onmicrosoft.com" {
		t.Errorf("environment must be used when the attribute is unset, got %q", got)
	}
	if got := envOrConfig("TEAMS_TENANT_ID", types.StringValue("")); got != "from-env.onmicrosoft.com" {
		t.Errorf("an empty string in HCL must fall back to the environment, got %q", got)
	}
}

func TestFileProblem(t *testing.T) {
	dir := t.TempDir()
	pfx := filepath.Join(dir, "app.pfx")
	if err := os.WriteFile(pfx, []byte("pfx"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		path string
		want string // substring of the problem, "" when the file is usable
	}{
		"empty path":   {"", ""},
		"usable file":  {pfx, ""},
		"missing file": {filepath.Join(dir, "missing.pfx"), "does not exist"},
		"folder":       {dir, "is a folder"},
	}
	for name, c := range cases {
		got := fileProblem("certificate_path", "TEAMS_CERTIFICATE_PATH", c.path)
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: fileProblem(%q) = %q, want %q", name, c.path, got, c.want)
		}
	}
}
