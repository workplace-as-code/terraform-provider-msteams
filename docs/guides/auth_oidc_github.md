---
page_title: "OIDC with GitHub Actions"
subcategory: "Guides"
---

# OIDC with GitHub Actions

With `auth_method = "oidc_github"`, a GitHub Actions job connects to Microsoft Teams **without any stored credential**: GitHub issues a short-lived OIDC token for the job, and Entra ID exchanges it for access tokens, if the token comes from the repository and environment the app registration trusts.

Before you start, create the app registration and assign its Teams role: steps 1 and 2 of the [Authentication guide](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/authentication). The app needs no certificate for this method.

## How It Works

```text
GitHub Actions job (permissions: id-token: write)
  1. The provider asks GitHub for the job's OIDC token (audience api://AzureADTokenExchange)
  2. The provider sends it to Entra ID as a client assertion for the app, and gets
     two access tokens valid about an hour: Microsoft Graph, and the Teams admin API
  3. PowerShell connects with Connect-MicrosoftTeams -AccessTokens
     A few minutes before the tokens expire, the provider gets new ones
     and starts a new PowerShell process with them
```

The Teams token needs no API permission on the app: the Teams Administrator role in the token is what counts, as with a certificate.

## Step 1 — Declare the Federated Credential

Trust a **protected environment**: only the branches you allow can use it, and each run can wait for a reviewer's approval.

1. In GitHub, create the environment: **Settings → Environments → New environment**, for example `production`. Set **Deployment branches** to your protected branch and add **Required reviewers**.
2. In Entra ID, open the app registration, then **Certificates & secrets → Federated credentials → Add credential**:
   - **Federated credential scenario**: GitHub Actions deploying Azure resources
   - **Organization** and **Repository**: your repository
   - **Entity type**: Environment, with the environment name
   - **Name**: for example `github-production`

With Microsoft Graph PowerShell:

```powershell
Connect-MgGraph -Scopes "Application.ReadWrite.All"
$app = Get-MgApplication -Filter "appId eq '<client-id>'"
New-MgApplicationFederatedIdentityCredential -ApplicationId $app.Id -BodyParameter @{
    name      = "github-production"
    issuer    = "https://token.actions.githubusercontent.com"
    subject   = "repo:contoso/teams-config:environment:production"
    audiences = @("api://AzureADTokenExchange")
}
```

### Subject Formats

Entra ID compares the subject **exactly**: no wildcard. Declare one federated credential per context (an app accepts up to 20).

GitHub writes the subject in one of two forms. Check which one your repository uses:

```bash
gh api repos/<org>/<repo>/actions/oidc/customization/sub
```

- `"use_immutable_subject": true`: the subject starts with the organization and repository IDs as well as their names, `repo:<org>@<org-id>/<repo>@<repo-id>`, given in `sub_claim_prefix`.
- Otherwise: the subject starts with `repo:<org>/<repo>`.

Then add the context:

| The job runs | Subject (with `repo:<org>/<repo>`, or the immutable prefix) |
|---|---|
| In an environment | `repo:<org>/<repo>:environment:<environment>` |
| On a branch, without environment | `repo:<org>/<repo>:ref:refs/heads/<branch>` |
| For a pull request, without environment | `repo:<org>/<repo>:pull_request` |

A job that declares an environment always gets the environment subject, whatever its branch.

## Step 2 — Configure the Workflow

The job needs `permissions: id-token: write`: without it GitHub does not let the job request a token.

```yaml
name: Terraform

on:
  push:
    branches: [main]

permissions:
  contents: read
  id-token: write

jobs:
  apply:
    runs-on: ubuntu-latest
    environment: production
    steps:
      - uses: actions/checkout@v7
      - uses: hashicorp/setup-terraform@v4

      - name: Install the MicrosoftTeams module
        shell: pwsh
        run: Install-PSResource -Name MicrosoftTeams -Scope CurrentUser -TrustRepository -Quiet

      - name: Terraform apply
        env:
          TEAMS_AUTH_METHOD: oidc_github
          TEAMS_TENANT_ID: contoso.onmicrosoft.com
          TEAMS_CLIENT_ID: ${{ vars.TEAMS_CLIENT_ID }}
        run: |
          terraform init
          terraform apply -auto-approve
```

Pin the actions to a commit SHA in production workflows.

## Step 3 — Configure the Provider

With the environment variables of the workflow above, the provider block can stay empty. In HCL:

```terraform
provider "msteams" {
  auth_method = "oidc_github"
  tenant_id   = "contoso.onmicrosoft.com"
  client_id   = var.client_id
}
```

Do not set a certificate at the same time: the provider refuses two methods at once.

## Security Best Practices

- **Trust an environment, not a branch**, and protect that environment with deployment branches and required reviewers. A pull request from a fork never gets its token.
- **One app per role**: a read app trusted by the plan environment, a write app trusted only by the deployment environment (see the [Authentication guide](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/authentication)).

## Troubleshooting

| Error | Cause | Fix |
|---|---|---|
| `Missing GitHub Actions OIDC variables` | The job lacks `permissions: id-token: write`, or Terraform does not run in GitHub Actions | Add the permission to the workflow or the job |
| `requesting the OIDC token from GitHub Actions failed (HTTP 403)` | The job may not request a token | Same: check `id-token: write`, also in reusable workflows |
| `AADSTS70021` or `AADSTS700213` | No federated credential matches the token's subject | Compare the subject with the table above, with the IDs if the repository uses immutable subjects |
| `AADSTS700211` | The issuer does not match | Use `https://token.actions.githubusercontent.com`, without a trailing slash |
| `AADSTS700212` | The audience does not match | Use `api://AzureADTokenExchange` in the federated credential |
| The connection works, then cmdlets fail with access denied | The app has no Teams role | Assign Teams Administrator (step 2 of the Authentication guide); it takes up to 30 minutes to apply |

More errors are listed in the [Troubleshooting guide](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/troubleshooting).
