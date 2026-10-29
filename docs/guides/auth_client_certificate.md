---
page_title: "Certificate Authentication"
subcategory: "Guides"
---

# Certificate Authentication

With `auth_method = "client_certificate"` (the default), the provider proves it is the app registration with a certificate: the app holds the public key, the provider holds the private key in a `.pfx`. It works anywhere Terraform runs, including internal CI runners that Entra ID cannot reach.

Before you start, create the app registration and assign its Teams role: steps 1 and 2 of the [Authentication guide](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/authentication).

## Step 1 — Generate a Certificate

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

A certificate issued by your internal PKI works the same way: the provider only needs the `.pfx`, Entra ID only the public key.

## Step 2 — Upload the Certificate to the App Registration

In the app registration, open **Certificates & secrets → Certificates → Upload certificate**, upload `terraform-msteams.cer` and note its expiry date: upload the next certificate before that date, or Terraform stops connecting.

## Step 3 — Configure the Provider

Give the provider exactly one certificate source.

**`.pfx` file:**

```terraform
provider "msteams" {
  tenant_id            = "contoso.onmicrosoft.com"
  client_id            = var.client_id
  certificate_path     = "/secrets/terraform-msteams.pfx"
  certificate_password = var.certificate_password
}
```

**Base64 content of the `.pfx`**, for a certificate injected by a vault or a masked CI variable, without writing a file on the runner:

```terraform
provider "msteams" {
  tenant_id            = "contoso.onmicrosoft.com"
  client_id            = var.client_id
  certificate_base64   = var.certificate_base64
  certificate_password = var.certificate_password
}
```

**Thumbprint of a certificate in the Windows certificate store:** the private key can stay non-exportable.

```terraform
provider "msteams" {
  tenant_id              = "contoso.onmicrosoft.com"
  client_id              = var.client_id
  certificate_thumbprint = "AABBCCDDEEFF..."
}
```

**Environment variables (recommended in CI):**

```bash
export TEAMS_TENANT_ID="contoso.onmicrosoft.com"
export TEAMS_CLIENT_ID="00000000-0000-0000-0000-000000000000"
export TEAMS_CERTIFICATE_PATH="/secrets/terraform-msteams.pfx"
export TEAMS_CERTIFICATE_PASSWORD="your-pfx-password"
```

Replace `TEAMS_CERTIFICATE_PATH` with `TEAMS_CERTIFICATE_BASE64` to pass the content instead of a path.

## The Base64 Certificate

`certificate_base64` takes the whole `.pfx` file, private key included, encoded in standard base64:

```bash
openssl base64 -A -in terraform-msteams.pfx
```

- The value may be cut in lines (`base64` without `-A`, a value copied from a vault): the provider removes the line breaks.
- The provider decodes it in memory and passes it to PowerShell through the process environment. No file is written.
- **Base64 is not encryption.** Store the value as a secret: a masked and protected CI variable, a vault, a sensitive Terraform variable.

From Azure Key Vault, read the certificate **as a secret**: the secret that Key Vault keeps under the certificate's name holds the whole `.pfx` in base64, without a password, when the certificate's content type is `application/x-pkcs12` (the default). The `azurerm_key_vault_certificate` data source does not fit: its `certificate_data_base64` is the public certificate only.

```terraform
data "azurerm_key_vault_secret" "teams" {
  name         = "terraform-msteams" # the certificate's name
  key_vault_id = var.key_vault_id
}

provider "msteams" {
  tenant_id          = "contoso.onmicrosoft.com"
  client_id          = var.client_id
  certificate_base64 = data.azurerm_key_vault_secret.teams.value
}
```

## Security Best Practices

- **Never commit** a `.pfx` or its password. Keep them in a vault or in CI secrets scoped to the deployment environment.
- **Prefer the Windows certificate store** on Windows runners: the private key can be marked non-exportable.
- **Track the expiry date**: an expired certificate stops every run at once.
- **Consider OIDC** where the CI platform supports it: nothing to store and nothing to renew. See [OIDC with GitHub Actions](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/auth_oidc_github).

## Troubleshooting

| Error | Cause | Fix |
|---|---|---|
| `Missing certificate` | No certificate source | Set `certificate_path`, `certificate_base64` or `certificate_thumbprint` (or their `TEAMS_CERTIFICATE_*` variable) |
| `Conflicting certificate settings` | Two sources at once, possibly one from an environment variable | Keep one source; unset the leftover `TEAMS_CERTIFICATE_*` variable |
| `Invalid certificate_base64` | The value is not base64 | Encode the whole `.pfx`: `openssl base64 -A -in app.pfx` |
| `AADSTS700027` | The certificate is not registered on the application, or has expired | Upload the `.cer` of the same certificate as the `.pfx`, and check its end date |

More errors are listed in the [Troubleshooting guide](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/troubleshooting).
