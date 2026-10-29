package provider

import (
	"encoding/base64"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/workplace-as-code/terraform-provider-msteams/internal/client"
)

// Values of auth_method. The names are those of the deploymenttheory/microsoft365
// provider, which most Microsoft 365 Terraform users already know.
const (
	authClientCertificate = "client_certificate"
	authOIDC              = "oidc"
	authOIDCGitHub        = "oidc_github"
	authManagedIdentity   = "managed_identity"
)

var authMethods = []string{authClientCertificate, authOIDC, authOIDCGitHub, authManagedIdentity}

// staticTokenHint explains the most likely cause when a token passed as a fixed
// value stops working during a run.
const staticTokenHint = "an OIDC token passed as a fixed value (oidc_token) cannot be renewed: if it expired during a long run, use oidc_token_file_path"

// resolveConfig turns the provider block and the TEAMS_* variables into the
// client configuration, or explains what is wrong with them. It does not
// contact Entra ID: the tokens are requested on the first call to Teams.
func resolveConfig(m teamsProviderModel) (client.Config, diag.Diagnostics) {
	var diags diag.Diagnostics

	cfg := client.Config{
		TenantID:              envOrConfig("TEAMS_TENANT_ID", m.TenantID),
		ClientID:              envOrConfig("TEAMS_CLIENT_ID", m.ClientID),
		CertificatePath:       envOrConfig("TEAMS_CERTIFICATE_PATH", m.CertificatePath),
		CertificatePassword:   envOrConfig("TEAMS_CERTIFICATE_PASSWORD", m.CertificatePassword),
		CertificateThumbprint: envOrConfig("TEAMS_CERTIFICATE_THUMBPRINT", m.CertificateThumbprint),
		// A base64 value cut in lines (base64 without -A, a vault) is accepted.
		CertificateBase64: strings.Join(strings.Fields(envOrConfig("TEAMS_CERTIFICATE_BASE64", m.CertificateBase64)), ""),
		Environment:       envOrConfig("TEAMS_ENVIRONMENT", m.Environment),
	}
	if cfg.Environment == "" {
		cfg.Environment = "global"
	}

	if cfg.TenantID == "" {
		diags.AddError("Missing tenant_id", "tenant_id is required. Set it in the provider block or via TEAMS_TENANT_ID.")
	}
	method := envOrConfig("TEAMS_AUTH_METHOD", m.AuthMethod)
	oidcToken := envOrConfig("TEAMS_OIDC_TOKEN", m.OIDCToken)
	oidcFile := envOrConfig("TEAMS_OIDC_TOKEN_FILE_PATH", m.OIDCTokenFilePath)
	if method == "" {
		// Without auth_method, TEAMS_OIDC_* variables left in the environment
		// are ignored, so that a working certificate configuration keeps
		// working. Values written in the provider block still count.
		method = authClientCertificate
		oidcToken, oidcFile = configValue(m.OIDCToken), configValue(m.OIDCTokenFilePath)
	}
	// A managed identity needs no client ID: without one, the system-assigned
	// identity of the machine connects.
	if cfg.ClientID == "" && method != authManagedIdentity {
		diags.AddError("Missing client_id", "client_id is required. Set it in the provider block or via TEAMS_CLIENT_ID.")
	}

	certificates := setNames(
		"certificate_path", cfg.CertificatePath,
		"certificate_base64", cfg.CertificateBase64,
		"certificate_thumbprint", cfg.CertificateThumbprint)
	oidcSources := setNames("oidc_token", oidcToken, "oidc_token_file_path", oidcFile)

	var assertion client.AssertionFunc
	hint := ""
	switch method {
	case authClientCertificate:
		if len(oidcSources) > 0 {
			diags.AddError("Conflicting authentication settings",
				fmt.Sprintf("%s only applies to auth_method = \"oidc\", but auth_method is %q: choose one method.", strings.Join(oidcSources, " and "), method))
		}
		switch {
		case len(certificates) == 0:
			diags.AddError("Missing certificate", "Either certificate_path, certificate_base64 or certificate_thumbprint is required. Set it in the provider block or via a TEAMS_CERTIFICATE_* variable.")
		case len(certificates) > 1:
			diags.AddError("Conflicting certificate settings",
				fmt.Sprintf("%s are all set: set only one of certificate_path, certificate_base64 and certificate_thumbprint (in the provider block or TEAMS_CERTIFICATE_* variables).", strings.Join(certificates, " and ")))
		}
		if problem := fileProblem("certificate_path", "TEAMS_CERTIFICATE_PATH", cfg.CertificatePath); problem != "" {
			// Checked here so that a wrong path fails at plan time with a clear message,
			// instead of an OpenSSL error from PowerShell at apply time.
			diags.AddError("Invalid certificate_path", problem)
		}
		if cfg.CertificateBase64 != "" {
			// The value is never quoted: it is the private key.
			if _, err := base64.StdEncoding.DecodeString(cfg.CertificateBase64); err != nil {
				diags.AddError("Invalid certificate_base64", "certificate_base64 is not valid base64: encode the whole .pfx file, for example with openssl base64 -A -in app.pfx.")
			}
		}

	case authOIDC, authOIDCGitHub:
		if len(certificates) > 0 {
			diags.AddError("Conflicting authentication settings",
				fmt.Sprintf("auth_method = %q does not use a certificate, but %s is set (in the provider block or a TEAMS_CERTIFICATE_* variable): choose one method.", method, strings.Join(certificates, " and ")))
		}
		if cfg.Environment != "global" && cfg.Environment != "gcc" {
			diags.AddError("OIDC not available in this environment",
				fmt.Sprintf("OIDC is only supported in the global and gcc environments for now, not in %q: use certificate authentication.", cfg.Environment))
		}
		if method == authOIDCGitHub {
			if len(oidcSources) > 0 {
				diags.AddError("Conflicting authentication settings",
					fmt.Sprintf("%s only applies to auth_method = \"oidc\": with \"oidc_github\" the token comes from GitHub Actions. Choose one method.", strings.Join(oidcSources, " and ")))
			}
			requestURL, requestToken := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL"), os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")
			if requestURL == "" || requestToken == "" {
				diags.AddError("Missing GitHub Actions OIDC variables",
					`auth_method = "oidc_github" needs the ACTIONS_ID_TOKEN_REQUEST_URL and ACTIONS_ID_TOKEN_REQUEST_TOKEN variables, which GitHub sets in a job that has "permissions: id-token: write". Add that permission to the workflow or the job, and run Terraform in GitHub Actions.`)
			}
			assertion = client.GitHubAssertion(requestURL, requestToken)
			break
		}
		switch {
		case len(oidcSources) == 0:
			diags.AddError("Missing OIDC token", `auth_method = "oidc" needs oidc_token or oidc_token_file_path, in the provider block or via TEAMS_OIDC_TOKEN or TEAMS_OIDC_TOKEN_FILE_PATH.`)
		case len(oidcSources) > 1:
			diags.AddError("Conflicting OIDC settings", "Set only one of oidc_token and oidc_token_file_path.")
		case oidcFile != "":
			if problem := fileProblem("oidc_token_file_path", "TEAMS_OIDC_TOKEN_FILE_PATH", oidcFile); problem != "" {
				diags.AddError("Invalid oidc_token_file_path", problem)
			}
			assertion = client.FileAssertion(oidcFile)
		default:
			assertion = client.StaticAssertion(oidcToken)
			hint = staticTokenHint
		}

	case authManagedIdentity:
		if conflicting := slices.Concat(certificates, oidcSources); len(conflicting) > 0 {
			diags.AddError("Conflicting authentication settings",
				fmt.Sprintf("auth_method = %q uses neither a certificate nor an OIDC token, but %s is set (in the provider block or a TEAMS_* variable): choose one method.", method, strings.Join(conflicting, " and ")))
		}
		if cfg.Environment != "global" && cfg.Environment != "gcc" {
			diags.AddError("Managed identity not available in this environment",
				fmt.Sprintf("The managed identity is only supported in the global and gcc environments for now, not in %q: use certificate authentication.", cfg.Environment))
		}
		if m.ClientID.IsUnknown() {
			// Unknown values fall back to TEAMS_CLIENT_ID or to nothing,
			// which would silently connect as another identity.
			diags.AddError("Unknown client_id",
				fmt.Sprintf("With auth_method = %q, client_id comes from a resource that is not created yet, so the provider cannot know which identity to use. Give it a known value (a variable, or a data source that reads an existing identity), or leave it out for the system-assigned identity.", method))
		} else if cfg.ClientID != "" && !guid.MatchString(cfg.ClientID) {
			diags.AddError("Invalid client_id",
				fmt.Sprintf("With auth_method = %q, client_id must be the client ID (a GUID) of a user-assigned managed identity, or be left empty for the system-assigned identity: %q is not a GUID.", method, cfg.ClientID))
		}

	default:
		diags.AddError("Invalid auth_method",
			fmt.Sprintf("auth_method %q is not supported. Valid values: %s.", method, strings.Join(authMethods, ", ")))
	}

	if diags.HasError() {
		return cfg, diags
	}
	switch {
	case method == authManagedIdentity:
		source, err := client.NewManagedIdentityTokenSource(cfg.ClientID)
		if err != nil {
			diags.AddError("Invalid managed identity configuration", err.Error())
			return cfg, diags
		}
		cfg.TokenSource = source
	case assertion != nil:
		source, err := client.NewOIDCTokenSource(cfg.TenantID, cfg.ClientID, assertion, hint)
		if err != nil {
			diags.AddError("Invalid OIDC configuration", err.Error())
			return cfg, diags
		}
		cfg.TokenSource = source
	}
	return cfg, diags
}

// guid matches a client ID.
var guid = regexp.MustCompile(`^[0-9a-fA-F]{8}-([0-9a-fA-F]{4}-){3}[0-9a-fA-F]{12}$`)

// configValue returns the value written in the provider block, ignoring the
// environment.
func configValue(v types.String) string {
	if v.IsNull() || v.IsUnknown() {
		return ""
	}
	return v.ValueString()
}

// setNames takes name, value pairs and returns the names whose value is not
// empty, in the order given.
func setNames(pairs ...string) []string {
	var names []string
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			names = append(names, pairs[i])
		}
	}
	return names
}

// fileProblem describes why the file set in attribute (or envVar) cannot be
// used, or returns "" when it is a readable file. An empty path is not checked.
func fileProblem(attribute, envVar, path string) string {
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		return fmt.Sprintf("%s %q does not exist. Set the path of the file, in the provider block or via %s. A relative path is resolved from the folder where Terraform runs.", attribute, path, envVar)
	case err != nil:
		return fmt.Sprintf("%s %q cannot be read: %v", attribute, path, err)
	case info.IsDir():
		return fmt.Sprintf("%s %q is a folder: set the path of the file itself.", attribute, path)
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Sprintf("%s %q cannot be read: %v", attribute, path, err)
	}
	_ = f.Close()
	return ""
}
