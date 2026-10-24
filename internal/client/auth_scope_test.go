package client

import (
	"os/exec"
	"strings"
	"testing"
)

// TestAuthLeaksNoVariable guards the scope of auth.ps1, which every bridge
// script dot-sources: a variable it assigns there overwrites the script's
// parameter of the same name, without regard to case.
func TestAuthLeaksNoVariable(t *testing.T) {
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh is not installed")
	}
	out, err := exec.Command("pwsh", "-NoProfile", "-NonInteractive",
		"-File", "testdata/auth_scope.ps1", "-Script", "../../bridge/scripts/auth.ps1").CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, name := range strings.Fields(string(out)) {
		if name != "ErrorActionPreference" {
			t.Errorf("auth.ps1 assigns $%s in the scope of the bridge scripts: move it into the connection block", name)
		}
	}
}
