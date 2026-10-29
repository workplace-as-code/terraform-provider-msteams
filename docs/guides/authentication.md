---
page_title: "Authentication Guide"
subcategory: "Guides"
---

# Authentication Guide

The Microsoft Teams provider connects with **app-only authentication** through the MicrosoftTeams PowerShell module: an Entra ID app registration or an Azure managed identity, not a user account. This page helps you choose how the provider proves who it is, then covers the setup every method shares: the identity and its Teams role.

## Choose a Method

| Method | `auth_method` | Works from | Secret stored where Terraform runs |
|---|---|---|---|
| Certificate: `.pfx` file, base64 content or Windows certificate store | `client_certificate` (default) | Anywhere: workstations, internal CI runners, servers | The `.pfx` and its password |
| OIDC from GitHub Actions | `oidc_github` | GitHub Actions jobs | Nothing |
| OIDC from another platform | `oidc` | A CI platform or cluster whose OIDC issuer Entra ID can reach over the internet | Nothing |
| Managed identity | `managed_identity` | Azure VMs and other Azure compute, Azure Arc-enabled servers | Nothing |

- **Internal CI (an on-premises GitLab or Jenkins), workstations, servers**: use a [certificate](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/auth_client_certificate). OIDC needs Entra ID to reach the token issuer over public HTTPS.
- **GitHub Actions**: use [`oidc_github`](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/auth_oidc_github). Nothing to store in GitHub, nothing to renew.
- **gitlab.com, Kubernetes, another platform with a public OIDC issuer**: use [`oidc`](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/auth_oidc).
- **Terraform runs on an Azure VM, a runner hosted in Azure or an Azure Arc-enabled server**: use [`managed_identity`](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/auth_managed_identity). No app registration and nothing to store: the machine's identity connects, and its guide replaces steps 1 to 3 below.

The provider does not offer client secrets.

## Step 1 — Create an App Registration in Entra ID

1. Go to **Entra ID → App registrations → New registration**
2. Name: `terraform-msteams` (or any name), supported account types: **Single tenant**, then **Register**
3. Note the **Application (client) ID**. For `tenant_id`, use either the **Directory (tenant) ID** or the `.onmicrosoft.com` domain: `Connect-MicrosoftTeams` accepts both
4. Under **API permissions → Add a permission → Microsoft Graph → Application permissions**, add `Organization.Read.All`
5. Click **Grant admin consent**, then remove the default delegated `User.Read` permission

Do not add permissions for "Skype and Teams Tenant Admin API": Microsoft documents that it can cause failures. The `*-Cs*` cmdlets used by this provider need no other Graph permission.

## Step 2 — Assign the Teams Administrator Role

1. Go to **Entra ID → Roles and administrators → Teams Administrator**
2. **Add assignments**, then search for the app registration by name and select it

The role is carried in the app's access token and controls what the PowerShell session can change. Role assignments can take up to 30 minutes to apply.

Unlike Exchange Online, Teams has no custom role groups: Teams Administrator is the narrowest built-in role that can change Teams policies and tenant-wide settings.

`msteams_group_policy_assignment` needs no other permission, unless the Teams Administrator role of the app is scoped to administrative units: it then also needs the Microsoft Graph `GroupMember.Read.All` application permission, with admin consent ([Microsoft documentation](https://learn.microsoft.com/microsoftteams/teams-powershell-application-authentication)).

## Step 3 — Give the App Its Credential

Follow the guide of the method you chose:

- [Certificate authentication](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/auth_client_certificate): generate a certificate, upload its public key to the app, give the `.pfx` to the provider.
- [OIDC with GitHub Actions](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/auth_oidc_github): declare which repository and environment the app trusts.
- [OIDC with any platform](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/auth_oidc): declare which issuer and subject the app trusts.
- [Managed identity](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/auth_managed_identity): no app registration; grant the permissions to the identity instead.

## Provider Settings

Values set in the `provider` block take precedence over environment variables.

| Attribute | Environment variable | Methods |
|---|---|---|
| `tenant_id` | `TEAMS_TENANT_ID` | All (required) |
| `client_id` | `TEAMS_CLIENT_ID` | All, required except with `managed_identity`, where it selects a user-assigned identity |
| `auth_method` | `TEAMS_AUTH_METHOD` | All (default `client_certificate`) |
| `certificate_path`, `certificate_base64`, `certificate_thumbprint`, `certificate_password` | `TEAMS_CERTIFICATE_*` | `client_certificate` |
| `oidc_token`, `oidc_token_file_path` | `TEAMS_OIDC_TOKEN`, `TEAMS_OIDC_TOKEN_FILE_PATH` | `oidc` |
| `environment` | `TEAMS_ENVIRONMENT` | All |

The provider checks these settings at plan time: an attribute that does not belong to the chosen method, or two certificate sources at once, fails with a message that says what to change. Without `auth_method`, `TEAMS_OIDC_*` variables left in the environment are ignored, so an existing certificate configuration keeps working.

The provider never passes secrets on the PowerShell command line: the certificate, its password and the access tokens reach the bridge scripts through the child process environment.

## Sovereign Clouds

```terraform
provider "msteams" {
  environment = "gcchigh" # global (default), gcc, gcchigh, dod
  # ...
}
```

`gcc` uses the commercial endpoints. `gcchigh` and `dod` map to `-TeamsEnvironmentName TeamsGCCH` and `TeamsDOD`, and need MicrosoftTeams module 5.0.1 or later. OIDC and managed identities are only supported in the `global` and `gcc` environments for now: use a certificate in the other clouds.

## Production: Separate Read and Write Apps

`terraform plan` only reads; `terraform apply` changes the tenant. When they run in different contexts, give them different apps:

| App | Used by | Permissions |
|---|---|---|
| `terraform-msteams-read` | `terraform plan` on pull requests, scheduled drift detection | `Organization.Read.All` + the **Teams Reader** Entra role |
| `terraform-msteams` | `terraform apply`, only from the protected branch after approval | `Organization.Read.All` + the **Teams Administrator** Entra role |

Teams Reader can read the whole Teams configuration and nothing outside Teams, unlike Global Reader, which reads the entire tenant. It lets the `Get-Cs*` cmdlets read policies, while `Set-Cs*` cmdlets are refused with `Forbidden`.

If the read credential leaks from a CI job, the Teams configuration can be read but not changed. With a certificate, keep the write certificate in a secret scoped to the deployment environment (for example a GitHub Actions environment with required reviewers). With OIDC, make the write app trust only that protected environment. Check that every resource you manage can be read with Teams Reader in your tenant before relying on the read app for drift detection.

For a single administrator testing on one tenant, one write app is enough.

## Storing Secrets Safely

Never commit the `.pfx` file or its password. Inject them at run time from your CI secret store (GitHub Actions secrets, Azure Key Vault, HashiCorp Vault, HCP Terraform sensitive variables).

## Troubleshooting

Connection errors (`AADSTS` codes), certificate, OIDC, managed identity and permission problems are covered in the [Troubleshooting guide](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/troubleshooting).
