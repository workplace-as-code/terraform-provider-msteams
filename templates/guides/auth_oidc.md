---
page_title: "OIDC with Any Platform"
subcategory: "Guides"
---

# OIDC with Any Platform

With `auth_method = "oidc"`, the provider exchanges an OIDC token issued by your CI platform or cluster for Microsoft Teams access tokens. Nothing secret is stored: the platform issues the token, and Entra ID accepts it if its issuer and subject match a federated credential of the app registration.

For GitHub Actions, use [`oidc_github`](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/auth_oidc_github) instead: the provider requests the token itself.

Before you start, create the app registration and assign its Teams role: steps 1 and 2 of the [Authentication guide](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/authentication).

## The Limit to Check First

Entra ID verifies the token's signature by downloading the issuer's OpenID configuration and public keys (`<issuer>/.well-known/openid-configuration`) **over the internet, with a publicly trusted certificate**. An internal GitLab, Jenkins or Kubernetes API server that Entra ID cannot reach cannot issue tokens it accepts: use [certificate authentication](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/auth_client_certificate) there.

gitlab.com, Azure Kubernetes Service (AKS) and other platforms with a public issuer work.

## Step 1 — Declare the Federated Credential

In Entra ID, open the app registration, then **Certificates & secrets → Federated credentials → Add credential → Other issuer**, and enter the issuer and the subject. The token must carry the audience `api://AzureADTokenExchange`. With Microsoft Graph PowerShell:

```powershell
Connect-MgGraph -Scopes "Application.ReadWrite.All"
$app = Get-MgApplication -Filter "appId eq '<client-id>'"
New-MgApplicationFederatedIdentityCredential -ApplicationId $app.Id -BodyParameter @{
    name      = "gitlab-main"
    issuer    = "https://gitlab.com"
    subject   = "project_path:contoso/teams-config:ref_type:branch:ref:main"
    audiences = @("api://AzureADTokenExchange")
}
```

## Step 2 — Give the Token to the Provider

Use exactly one of the two sources:

| Attribute | Environment variable | Use it when |
|---|---|---|
| `oidc_token` | `TEAMS_OIDC_TOKEN` | The platform puts the token in a variable |
| `oidc_token_file_path` | `TEAMS_OIDC_TOKEN_FILE_PATH` | The platform writes the token to a file |

The file is read again each time the provider needs new access tokens, so a platform that renews the file (Kubernetes) keeps a long run working. A token passed as a value cannot be renewed: if the run outlasts it, the provider fails with a message that says so.

### GitLab CI (gitlab.com)

```yaml
apply:
  image: registry.example.com/terraform-pwsh:latest # Terraform and PowerShell 7
  id_tokens:
    TEAMS_OIDC_TOKEN:
      aud: api://AzureADTokenExchange
  variables:
    TEAMS_AUTH_METHOD: oidc
    TEAMS_TENANT_ID: contoso.onmicrosoft.com
  script:
    - pwsh -c "Install-PSResource -Name MicrosoftTeams -Scope CurrentUser -TrustRepository -Quiet"
    - terraform init
    - terraform apply -auto-approve
```

Set `TEAMS_CLIENT_ID` as a CI/CD variable of the project. The GitLab subject has the form `project_path:<group>/<project>:ref_type:branch:ref:<branch>`. Restrict the job to protected branches. A GitLab ID token stays valid for the job's timeout: keep the timeout above the run's duration.

### Kubernetes (AKS workload identity)

A pod with workload identity receives a projected token file, renewed by Kubernetes:

```bash
export TEAMS_AUTH_METHOD=oidc
export TEAMS_TENANT_ID=contoso.onmicrosoft.com
export TEAMS_CLIENT_ID=<client-id>
export TEAMS_OIDC_TOKEN_FILE_PATH="$AZURE_FEDERATED_TOKEN_FILE"
```

The federated credential uses the cluster's OIDC issuer URL and the subject `system:serviceaccount:<namespace>:<service-account>`.

## Step 3 — Configure the Provider

```terraform
provider "msteams" {
  auth_method          = "oidc"
  tenant_id            = "contoso.onmicrosoft.com"
  client_id            = var.client_id
  oidc_token_file_path = "/var/run/secrets/tokens/teams.jwt"
}
```

## Troubleshooting

| Error | Cause | Fix |
|---|---|---|
| `Missing OIDC token` | Neither `oidc_token` nor `oidc_token_file_path` is set | Set one of them, or its environment variable |
| `Conflicting OIDC settings` | Both are set | Keep one |
| `Invalid oidc_token_file_path` | The file does not exist or cannot be read | Check the path and the permissions of the user running Terraform |
| `AADSTS70021` or `AADSTS700213` | No federated credential matches the token's subject | Decode the token's payload (the middle part, base64) and compare `sub` with the federated credential |
| `AADSTS700211` | The issuer does not match | Compare `iss` with the federated credential, character for character |
| `AADSTS700212` | The audience does not match | Request the token with the audience `api://AzureADTokenExchange` |
| `AADSTS700024` | The token has expired | Use a token file that the platform renews, or a shorter run |

More errors are listed in the [Troubleshooting guide](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/troubleshooting).
