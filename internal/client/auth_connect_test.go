package client

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// TestAuthConnects checks how auth.ps1 calls Connect-MicrosoftTeams with the
// two access tokens (OIDC or managed identity) and with a certificate passed
// in base64.
func TestAuthConnects(t *testing.T) {
	if _, err := exec.LookPath("pwsh"); err != nil {
		t.Skip("pwsh is not installed")
	}
	type result struct {
		Connected struct {
			TenantId      string
			ApplicationId string
			AccessTokens  []string
			Thumbprint    string
		} `json:"connected"`
		ExpectedThumbprint string `json:"expectedThumbprint"`
	}
	connect := func(t *testing.T, name string) result {
		t.Helper()
		out, err := exec.Command("pwsh", "-NoProfile", "-NonInteractive",
			"-File", "testdata/auth_connect.ps1", "-Script", "../../bridge/scripts/auth.ps1", "-Case", name).CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		var r result
		if err := json.Unmarshal(out, &r); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return r
	}

	// token-noclientid: a system-assigned managed identity has no client ID.
	// Connect-MicrosoftTeams takes the Graph token first, then the Teams one,
	// and fails with "Not supported tenant type" when -TenantId comes too.
	for _, name := range []string{"token", "token-noclientid"} {
		t.Run(name, func(t *testing.T) {
			r := connect(t, name)
			if strings.Join(r.Connected.AccessTokens, ",") != "graph-1,teams-1" || r.Connected.TenantId != "" ||
				r.Connected.ApplicationId != "" || r.Connected.Thumbprint != "" {
				t.Errorf("Connect-MicrosoftTeams got %+v", r.Connected)
			}
		})
	}
	for _, name := range []string{"base64", "base64-wrapped", "base64-nopassword"} {
		t.Run(name, func(t *testing.T) {
			r := connect(t, name)
			if r.ExpectedThumbprint == "" || r.Connected.Thumbprint != r.ExpectedThumbprint || len(r.Connected.AccessTokens) != 0 {
				t.Errorf("Connect-MicrosoftTeams got %+v, want the certificate %s", r.Connected, r.ExpectedThumbprint)
			}
		})
	}
}
