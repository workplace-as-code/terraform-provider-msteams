package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/list"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/workplace-as-code/terraform-provider-msteams/internal/client"
	"github.com/workplace-as-code/terraform-provider-msteams/internal/resources"
)

var _ provider.ProviderWithListResources = &teamsProvider{}

type teamsProvider struct {
	version string
}

// teamsProviderModel represents the HCL provider {} block. Every field is
// optional: it can also come from a TEAMS_* environment variable.
type teamsProviderModel struct {
	TenantID              types.String `tfsdk:"tenant_id"`
	ClientID              types.String `tfsdk:"client_id"`
	AuthMethod            types.String `tfsdk:"auth_method"`
	CertificatePath       types.String `tfsdk:"certificate_path"`
	CertificatePassword   types.String `tfsdk:"certificate_password"`
	CertificateThumbprint types.String `tfsdk:"certificate_thumbprint"`
	CertificateBase64     types.String `tfsdk:"certificate_base64"`
	OIDCToken             types.String `tfsdk:"oidc_token"`
	OIDCTokenFilePath     types.String `tfsdk:"oidc_token_file_path"`
	Environment           types.String `tfsdk:"environment"`
}

// New returns a factory function that creates the provider.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &teamsProvider{version: version}
	}
}

func (p *teamsProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "msteams"
	resp.Version = p.version
}

func (p *teamsProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The Microsoft Teams provider manages Teams policies and tenant settings as code. It authenticates app-only through the MicrosoftTeams PowerShell module, with a certificate, OIDC or an Azure managed identity.",
		Attributes: map[string]schema.Attribute{
			"tenant_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Entra ID tenant ID or `.onmicrosoft.com` domain. Required, either here or via the `TEAMS_TENANT_ID` environment variable.",
			},
			"client_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Entra ID app registration client ID. Required, either here or via the `TEAMS_CLIENT_ID` environment variable, except with `auth_method = \"managed_identity\"`: there it is the client ID of a user-assigned managed identity, and left empty the system-assigned identity connects.",
			},
			"auth_method": schema.StringAttribute{
				Optional:            true,
				Validators:          []validator.String{stringvalidator.OneOf(authMethods...)},
				MarkdownDescription: "How the provider authenticates. Valid values: `client_certificate` (default: a certificate, see `certificate_path`, `certificate_base64` and `certificate_thumbprint`), `oidc` (an OIDC token from any platform, see `oidc_token` and `oidc_token_file_path`), `oidc_github` (the OIDC token of the GitHub Actions job, which needs `permissions: id-token: write`), `managed_identity` (the managed identity of the Azure resource or Azure Arc-enabled server running Terraform, see `client_id`). Can also be set via `TEAMS_AUTH_METHOD` environment variable.",
			},
			"certificate_path": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Path to the `.pfx` certificate file for app-only (CBA) authentication. Use only one of `certificate_path`, `certificate_base64` and `certificate_thumbprint`. Can also be set via `TEAMS_CERTIFICATE_PATH` environment variable.",
			},
			"certificate_base64": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Content of the `.pfx` certificate file, private key included, encoded in base64 (for example `openssl base64 -A -in app.pfx`). Decoded in memory: no file is written. Use it to pass the certificate from a vault or a masked CI variable. Use only one of `certificate_path`, `certificate_base64` and `certificate_thumbprint`. Can also be set via `TEAMS_CERTIFICATE_BASE64` environment variable.",
			},
			"certificate_password": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Password for the `.pfx` certificate, given as a file or in base64. Can also be set via `TEAMS_CERTIFICATE_PASSWORD` environment variable.",
			},
			"certificate_thumbprint": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Thumbprint of a certificate already loaded in the Windows certificate store. Use only one of `certificate_path`, `certificate_base64` and `certificate_thumbprint`. Can also be set via `TEAMS_CERTIFICATE_THUMBPRINT` environment variable.",
			},
			"oidc_token": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "With `auth_method = \"oidc\"`: the OIDC token that Entra ID exchanges for access tokens (for example a GitLab `id_tokens` value). It cannot be renewed during the run: prefer `oidc_token_file_path` when the platform writes the token to a file. Use only one of `oidc_token` and `oidc_token_file_path`. Can also be set via `TEAMS_OIDC_TOKEN` environment variable.",
			},
			"oidc_token_file_path": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "With `auth_method = \"oidc\"`: path to a file that contains the OIDC token, read again each time new access tokens are needed (Kubernetes renews projected tokens). Use only one of `oidc_token` and `oidc_token_file_path`. Can also be set via `TEAMS_OIDC_TOKEN_FILE_PATH` environment variable.",
			},
			"environment": schema.StringAttribute{
				Optional:            true,
				Validators:          []validator.String{stringvalidator.OneOf("global", "gcc", "gcchigh", "dod")},
				MarkdownDescription: "M365 cloud environment. Valid values: `global` (default), `gcc`, `gcchigh`, `dod`. Can also be set via `TEAMS_ENVIRONMENT` environment variable.",
			},
		},
	}
}

func (p *teamsProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config teamsProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg, diags := resolveConfig(config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiClient, err := client.New(ctx, cfg)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create Teams client", err.Error())
		return
	}

	resp.DataSourceData = apiClient
	resp.ResourceData = apiClient
	resp.ListResourceData = apiClient
}

func (p *teamsProvider) Resources(_ context.Context) []func() resource.Resource {
	return resources.Resources()
}

// ListResources lists the existing objects for terraform query (Terraform 1.14+).
func (p *teamsProvider) ListResources(_ context.Context) []func() list.ListResource {
	return resources.ListResources()
}

func (p *teamsProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return resources.DataSources()
}

// envOrConfig returns the HCL config value if set, otherwise the environment
// variable. An explicit provider block always wins, as in the HashiCorp
// providers, so a stray variable in a CI runner cannot silently redirect
// Terraform to another tenant.
func envOrConfig(envKey string, configVal types.String) string {
	if !configVal.IsNull() && !configVal.IsUnknown() && configVal.ValueString() != "" {
		return configVal.ValueString()
	}
	return os.Getenv(envKey)
}
