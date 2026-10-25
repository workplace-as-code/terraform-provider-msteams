---
page_title: "Authentication Guide"
subcategory: "Guides"
---

# Authentication Guide

The Microsoft Teams provider connects with **certificate-based app-only authentication** through the MicrosoftTeams PowerShell module (`Connect-MicrosoftTeams -ApplicationId ... -Certificate ...`). No user account or interactive sign-in is involved.

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

## Step 3 — Generate a Certificate

**Option A — Self-signed, with PowerShell on Windows**

```powershell
$cert = New-SelfSignedCertificate `
    -Subject "CN=terraform-msteams" `
    -CertStoreLocation "Cert:\CurrentUser\My" `
    -KeyExportPolicy Exportable `
    -KeySpec KeyExchange `
    -KeyLength 2048 `
    -KeyAlgorithm RSA `
    -HashAlgorithm SHA256 `
    -NotAfter (Get-Date).AddYears(2)

# Public key (.cer): upload it to the app registration
Export-Certificate -Cert $cert -FilePath "terraform-msteams.cer"

# Private key (.pfx): used by the provider
$password = Read-Host -AsSecureString "PFX password"
Export-PfxCertificate -Cert $cert -FilePath "terraform-msteams.pfx" -Password $password
```

**Option B — OpenSSL (Linux, macOS, CI)**

```bash
openssl req -x509 -newkey rsa:2048 -keyout terraform-msteams.key \
    -out terraform-msteams.cer -days 730 -nodes -subj "/CN=terraform-msteams"

openssl pkcs12 -export -out terraform-msteams.pfx \
    -inkey terraform-msteams.key -in terraform-msteams.cer

rm terraform-msteams.key   # the private key now lives only in the .pfx
```

## Step 4 — Upload the Certificate

In the app registration, open **Certificates & secrets → Certificates → Upload certificate** and upload `terraform-msteams.cer`.

## Step 5 — Configure the Provider

**With a .pfx file (Linux, macOS, Windows):**

```terraform
provider "msteams" {
  tenant_id            = "contoso.onmicrosoft.com"
  client_id            = var.client_id
  certificate_path     = "/secrets/terraform-msteams.pfx"
  certificate_password = var.certificate_password
}
```

**With a certificate in the Windows certificate store:**

```terraform
provider "msteams" {
  tenant_id              = "contoso.onmicrosoft.com"
  client_id              = var.client_id
  certificate_thumbprint = "AABBCCDDEEFF..."
}
```

**With environment variables (recommended in CI):**

```bash
export TEAMS_TENANT_ID="contoso.onmicrosoft.com"
export TEAMS_CLIENT_ID="00000000-0000-0000-0000-000000000000"
export TEAMS_CERTIFICATE_PATH="/secrets/terraform-msteams.pfx"
export TEAMS_CERTIFICATE_PASSWORD="your-pfx-password"
```

Values set in the `provider` block take precedence over environment variables.

The provider never puts the certificate password on the PowerShell command line: it reaches the bridge scripts through the child process environment.

## Sovereign Clouds

```terraform
provider "msteams" {
  environment = "gcchigh" # global (default), gcc, gcchigh, dod
  # ...
}
```

`gcc` uses the commercial endpoints. `gcchigh` and `dod` map to `-TeamsEnvironmentName TeamsGCCH` and `TeamsDOD`, and need MicrosoftTeams module 5.0.1 or later.

## Production: Separate Read and Write Apps

`terraform plan` only reads; `terraform apply` changes the tenant. When they run in different contexts, give them different apps:

| App | Used by | Permissions |
|---|---|---|
| `terraform-msteams-read` | `terraform plan` on pull requests, scheduled drift detection | `Organization.Read.All` + the **Teams Reader** Entra role |
| `terraform-msteams` | `terraform apply`, only from the protected branch after approval | `Organization.Read.All` + the **Teams Administrator** Entra role |

Teams Reader can read the whole Teams configuration and nothing outside Teams, unlike Global Reader, which reads the entire tenant. With app-only certificate authentication, it lets the `Get-Cs*` cmdlets read policies, while `Set-Cs*` cmdlets are refused with `Forbidden`.

If the read certificate leaks from a CI job, the Teams configuration can be read but not changed. Keep the write certificate in a secret scoped to the deployment environment (for example a GitHub Actions environment with required reviewers). Check that every resource you manage can be read with Teams Reader in your tenant before relying on the read app for drift detection.

For a single administrator testing on one tenant, one write app is enough.

## Storing Secrets Safely

Never commit the `.pfx` file or its password. Inject them at run time from your CI secret store (GitHub Actions secrets, Azure Key Vault, HashiCorp Vault, HCP Terraform sensitive variables).

## Troubleshooting

Connection errors (`AADSTS` codes), certificate and permission problems are covered in the [Troubleshooting guide](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/troubleshooting).
