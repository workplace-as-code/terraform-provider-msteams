package provider

import (
	"context"
	"fmt"
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

type teamsProviderModel struct {
	TenantID              types.String `tfsdk:"tenant_id"`
	ClientID              types.String `tfsdk:"client_id"`
	CertificatePath       types.String `tfsdk:"certificate_path"`
	CertificatePassword   types.String `tfsdk:"certificate_password"`
	CertificateThumbprint types.String `tfsdk:"certificate_thumbprint"`
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
		MarkdownDescription: "The Microsoft Teams provider manages Teams policies and tenant settings as code. It uses app-only certificate-based authentication through the MicrosoftTeams PowerShell module.",
		Attributes: map[string]schema.Attribute{
			"tenant_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Entra ID tenant ID or `.onmicrosoft.com` domain. Required, either here or via the `TEAMS_TENANT_ID` environment variable.",
			},
			"client_id": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Entra ID app registration client ID. Required, either here or via the `TEAMS_CLIENT_ID` environment variable.",
			},
			"certificate_path": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Path to the `.pfx` certificate file for app-only (CBA) authentication. Use this or `certificate_thumbprint`. Can also be set via `TEAMS_CERTIFICATE_PATH` environment variable.",
			},
			"certificate_password": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Password for the `.pfx` certificate file. Can also be set via `TEAMS_CERTIFICATE_PASSWORD` environment variable.",
			},
			"certificate_thumbprint": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Thumbprint of a certificate already loaded in the Windows certificate store. Use this or `certificate_path`. Can also be set via `TEAMS_CERTIFICATE_THUMBPRINT` environment variable.",
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

	var cfg client.Config
	cfg.TenantID = envOrConfig("TEAMS_TENANT_ID", config.TenantID)
	cfg.ClientID = envOrConfig("TEAMS_CLIENT_ID", config.ClientID)
	cfg.CertificatePath = envOrConfig("TEAMS_CERTIFICATE_PATH", config.CertificatePath)
	cfg.CertificatePassword = envOrConfig("TEAMS_CERTIFICATE_PASSWORD", config.CertificatePassword)
	cfg.CertificateThumbprint = envOrConfig("TEAMS_CERTIFICATE_THUMBPRINT", config.CertificateThumbprint)
	cfg.Environment = envOrConfig("TEAMS_ENVIRONMENT", config.Environment)
	if cfg.Environment == "" {
		cfg.Environment = "global"
	}

	if cfg.TenantID == "" {
		resp.Diagnostics.AddError("Missing tenant_id", "tenant_id is required. Set it in the provider block or via TEAMS_TENANT_ID.")
	}
	if cfg.ClientID == "" {
		resp.Diagnostics.AddError("Missing client_id", "client_id is required. Set it in the provider block or via TEAMS_CLIENT_ID.")
	}
	if cfg.CertificatePath == "" && cfg.CertificateThumbprint == "" {
		resp.Diagnostics.AddError("Missing certificate", "Either certificate_path or certificate_thumbprint is required.")
	} else if problem := certificateFileProblem(cfg.CertificatePath); problem != "" {
		// Checked here so that a wrong path fails at plan time with a clear message,
		// instead of an OpenSSL error from PowerShell at apply time.
		resp.Diagnostics.AddError("Invalid certificate_path", problem)
	}

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
	return []func() datasource.DataSource{}
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

// certificateFileProblem describes why the .pfx file at path cannot be used, or
// returns "" when it is a readable file. An empty path is not checked: the
// certificate then comes from certificate_thumbprint.
func certificateFileProblem(path string) string {
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		return fmt.Sprintf("certificate_path %q does not exist. Set the path of the app registration's .pfx file, in the provider block or via TEAMS_CERTIFICATE_PATH. A relative path is resolved from the folder where Terraform runs.", path)
	case err != nil:
		return fmt.Sprintf("certificate_path %q cannot be read: %v", path, err)
	case info.IsDir():
		return fmt.Sprintf("certificate_path %q is a folder: set the path of the .pfx file itself.", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Sprintf("certificate_path %q cannot be read: %v", path, err)
	}
	_ = f.Close()
	return ""
}
