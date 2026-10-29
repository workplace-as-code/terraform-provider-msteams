package provider

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

// authEnv lists every variable resolveConfig reads. Each case starts with all
// of them empty, so the shell running the tests (a sourced .env) cannot change
// the result.
var authEnv = []string{
	"TEAMS_TENANT_ID", "TEAMS_CLIENT_ID", "TEAMS_AUTH_METHOD", "TEAMS_ENVIRONMENT",
	"TEAMS_CERTIFICATE_PATH", "TEAMS_CERTIFICATE_PASSWORD", "TEAMS_CERTIFICATE_THUMBPRINT", "TEAMS_CERTIFICATE_BASE64",
	"TEAMS_OIDC_TOKEN", "TEAMS_OIDC_TOKEN_FILE_PATH",
	"ACTIONS_ID_TOKEN_REQUEST_URL", "ACTIONS_ID_TOKEN_REQUEST_TOKEN",
}

// secrets are values that must never appear in a diagnostic.
const (
	secretToken  = "jwt-secret-value"
	secretBase64 = "c2VjcmV0LXBmeA=="
)

func TestResolveConfig(t *testing.T) {
	dir := t.TempDir()
	pfx := filepath.Join(dir, "app.pfx")
	tokenFile := filepath.Join(dir, "token.jwt")
	for _, f := range []string{pfx, tokenFile} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	base := map[string]string{"tenant_id": "contoso.onmicrosoft.com", "client_id": "00000000-0000-0000-0000-000000000001"}
	github := map[string]string{"ACTIONS_ID_TOKEN_REQUEST_URL": "https://token.example/x?api-version=2.0", "ACTIONS_ID_TOKEN_REQUEST_TOKEN": "req"}

	cases := map[string]struct {
		attrs           map[string]string
		env             map[string]string
		wantError       string // substring of a diagnostic, "" when the configuration is valid
		wantTokenSource bool
		noClientID      bool // remove client_id from the base attributes
		unknownClientID bool // client_id computed from a resource not created yet
		check           func(t *testing.T, base64 string)
	}{
		"default is client_certificate": {
			attrs: map[string]string{"certificate_path": pfx},
		},
		"leftover oidc env is ignored by default": {
			attrs: map[string]string{"certificate_path": pfx},
			env:   map[string]string{"TEAMS_OIDC_TOKEN": secretToken},
		},
		"oidc_github": {
			attrs:           map[string]string{"auth_method": "oidc_github"},
			env:             github,
			wantTokenSource: true,
		},
		"oidc_github from env": {
			env:             merge(github, map[string]string{"TEAMS_AUTH_METHOD": "oidc_github"}),
			wantTokenSource: true,
		},
		"oidc_github without GitHub variables": {
			attrs:     map[string]string{"auth_method": "oidc_github"},
			wantError: "id-token: write",
		},
		"oidc with oidc_token": {
			attrs:           map[string]string{"auth_method": "oidc", "oidc_token": secretToken},
			wantTokenSource: true,
		},
		"oidc with oidc_token_file_path": {
			attrs:           map[string]string{"auth_method": "oidc", "oidc_token_file_path": tokenFile},
			wantTokenSource: true,
		},
		"oidc with both sources": {
			attrs:     map[string]string{"auth_method": "oidc", "oidc_token": secretToken, "oidc_token_file_path": tokenFile},
			wantError: "only one",
		},
		"oidc without source": {
			attrs:     map[string]string{"auth_method": "oidc"},
			wantError: "oidc_token",
		},
		"oidc_token_file_path missing file": {
			attrs:     map[string]string{"auth_method": "oidc", "oidc_token_file_path": filepath.Join(dir, "missing.jwt")},
			wantError: filepath.Join(dir, "missing.jwt"),
		},
		"oidc with a certificate": {
			attrs:     map[string]string{"auth_method": "oidc", "oidc_token": secretToken},
			env:       map[string]string{"TEAMS_CERTIFICATE_PATH": pfx},
			wantError: "choose one",
		},
		"client_certificate with oidc_token": {
			attrs:     map[string]string{"auth_method": "client_certificate", "certificate_path": pfx, "oidc_token": secretToken},
			wantError: "choose one",
		},
		"oidc on gcchigh": {
			attrs:     map[string]string{"auth_method": "oidc_github", "environment": "gcchigh"},
			env:       github,
			wantError: "only supported in the global and gcc environments",
		},
		"oidc on gcc": {
			attrs:           map[string]string{"auth_method": "oidc_github", "environment": "gcc"},
			env:             github,
			wantTokenSource: true,
		},
		"unknown auth_method from env": {
			attrs:     map[string]string{"certificate_path": pfx},
			env:       map[string]string{"TEAMS_AUTH_METHOD": "foo"},
			wantError: "client_certificate, oidc, oidc_github, managed_identity",
		},
		"managed_identity system-assigned": {
			attrs:           map[string]string{"auth_method": "managed_identity"},
			noClientID:      true,
			wantTokenSource: true,
		},
		"managed_identity user-assigned": {
			attrs:           map[string]string{"auth_method": "managed_identity"},
			wantTokenSource: true,
		},
		"managed_identity user-assigned upper-case client_id": {
			attrs:           map[string]string{"auth_method": "managed_identity", "client_id": "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"},
			wantTokenSource: true,
		},
		"managed_identity from env": {
			env:             map[string]string{"TEAMS_AUTH_METHOD": "managed_identity"},
			noClientID:      true,
			wantTokenSource: true,
		},
		"managed_identity with a non-GUID client_id": {
			attrs:     map[string]string{"auth_method": "managed_identity", "client_id": "terraform-msteams"},
			wantError: "client ID (a GUID)",
		},
		"managed_identity with certificate_path": {
			attrs:     map[string]string{"auth_method": "managed_identity", "certificate_path": pfx},
			wantError: "Conflicting authentication settings",
		},
		"managed_identity with TEAMS_CERTIFICATE_PATH": {
			attrs:     map[string]string{"auth_method": "managed_identity"},
			env:       map[string]string{"TEAMS_CERTIFICATE_PATH": pfx},
			wantError: "Conflicting authentication settings",
		},
		"managed_identity with TEAMS_OIDC_TOKEN": {
			attrs:     map[string]string{"auth_method": "managed_identity"},
			env:       map[string]string{"TEAMS_OIDC_TOKEN": secretToken},
			wantError: "Conflicting authentication settings",
		},
		"managed_identity on gcchigh": {
			attrs:     map[string]string{"auth_method": "managed_identity", "environment": "gcchigh"},
			wantError: "only supported in the global and gcc environments",
		},
		"managed_identity with an unknown client_id": {
			attrs:           map[string]string{"auth_method": "managed_identity"},
			unknownClientID: true,
			wantError:       "Unknown client_id",
		},
		"client_certificate without client_id": {
			attrs:      map[string]string{"certificate_path": pfx},
			noClientID: true,
			wantError:  "Missing client_id",
		},
		"two certificate sources": {
			attrs:     map[string]string{"certificate_path": pfx, "certificate_base64": secretBase64},
			wantError: "only one",
		},
		"no certificate": {
			wantError: "certificate_path, certificate_base64 or certificate_thumbprint",
		},
		"certificate base64": {
			attrs: map[string]string{"certificate_base64": secretBase64},
			check: func(t *testing.T, b64 string) {
				if b64 != secretBase64 {
					t.Errorf("CertificateBase64 = %q", b64)
				}
			},
		},
		"certificate base64 with newlines": {
			env: map[string]string{"TEAMS_CERTIFICATE_BASE64": "c2VjcmV0\nLXBmeA==\n"},
			check: func(t *testing.T, b64 string) {
				if b64 != secretBase64 {
					t.Errorf("CertificateBase64 = %q, want the blanks removed", b64)
				}
			},
		},
		"invalid base64": {
			attrs:     map[string]string{"certificate_base64": "not base64 " + secretBase64},
			wantError: "not valid base64",
		},
		"tenant GUID with oidc": {
			attrs:           map[string]string{"tenant_id": "11111111-2222-3333-4444-555555555555", "auth_method": "oidc", "oidc_token": secretToken},
			wantTokenSource: true,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			for _, k := range authEnv {
				t.Setenv(k, "")
			}
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			attrs := merge(base, c.attrs)
			if c.noClientID {
				delete(attrs, "client_id")
			}
			m := model(attrs)
			if c.unknownClientID {
				m.ClientID = types.StringUnknown()
			}
			cfg, diags := resolveConfig(m)

			var messages []string
			for _, d := range diags {
				messages = append(messages, d.Summary()+": "+d.Detail())
			}
			all := strings.Join(messages, "\n")
			if strings.Contains(all, secretToken) || strings.Contains(all, secretBase64) {
				t.Errorf("a diagnostic leaks a secret:\n%s", all)
			}

			switch {
			case c.wantError != "" && !strings.Contains(all, c.wantError):
				t.Fatalf("want an error containing %q, got:\n%s", c.wantError, all)
			case c.wantError == "" && diags.HasError():
				t.Fatalf("unexpected error:\n%s", all)
			case c.wantError != "":
				return
			}
			if got := cfg.TokenSource != nil; got != c.wantTokenSource {
				t.Errorf("TokenSource set = %v, want %v", got, c.wantTokenSource)
			}
			if c.check != nil {
				c.check(t, cfg.CertificateBase64)
			}
		})
	}
}

// model builds the provider block from attribute values; absent attributes are null.
func model(attrs map[string]string) teamsProviderModel {
	v := func(name string) types.String {
		if s, ok := attrs[name]; ok {
			return types.StringValue(s)
		}
		return types.StringNull()
	}
	return teamsProviderModel{
		TenantID:              v("tenant_id"),
		ClientID:              v("client_id"),
		AuthMethod:            v("auth_method"),
		CertificatePath:       v("certificate_path"),
		CertificatePassword:   v("certificate_password"),
		CertificateThumbprint: v("certificate_thumbprint"),
		CertificateBase64:     v("certificate_base64"),
		OIDCToken:             v("oidc_token"),
		OIDCTokenFilePath:     v("oidc_token_file_path"),
		Environment:           v("environment"),
	}
}

func merge(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}
