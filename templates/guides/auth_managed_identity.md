---
page_title: "Managed Identity"
subcategory: "Guides"
---

# Managed Identity

With `auth_method = "managed_identity"`, the provider connects as the **managed identity** of the machine running Terraform: an Azure VM, a CI runner hosted in Azure, or a server outside Azure connected to **Azure Arc**. Nothing is stored and nothing expires: Azure creates the identity, keeps its credential and rotates it.

## The Limit to Check First

A managed identity only exists on an Azure resource or an Azure Arc-enabled server. A workstation or an on-premises CI runner that is not connected to Arc has none: use [certificate authentication](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/auth_client_certificate) there.

| Where Terraform runs | Identities available |
|---|---|
| Azure VM, VM scale set, other Azure compute | System-assigned and user-assigned |
| Azure Arc-enabled server (on-premises or another cloud) | System-assigned |
| Workstation, on-premises runner without Arc | None: use a certificate |

- **System-assigned**: created with the resource and deleted with it. Leave `client_id` empty.
- **User-assigned**: a separate Azure resource that you attach to one or more machines, so several runners can share one identity and its permissions. Set `client_id` to the identity's **client ID**.

## How It Works

```text
1. The provider asks the local identity endpoint for two tokens: Graph and the Teams admin API
     Azure VM:  http://169.254.169.254 (reachable only from the VM itself)
     Azure Arc: http://localhost:40342 (the Connected Machine agent)
2. PowerShell connects with Connect-MicrosoftTeams -AccessTokens
```

The provider asks for new tokens before the current ones expire, so a long run keeps working. `AZURE_CLIENT_ID` and the other `AZURE_*` variables are ignored: only `client_id` selects a user-assigned identity.

## Step 1 — Get the Identity

You need two values of the identity, shown in **Entra admin center → Enterprise applications**, with the **Application type** filter set to **Managed Identities**: the **Application ID** (the client ID, for `client_id` with a user-assigned identity) and the **Object ID** (to grant the permissions).

**System-assigned identity of an Azure VM**: in the Azure portal, open the VM, then **Security → Identity → System assigned → Status: On**. With the Azure CLI:

```bash
az vm identity assign --resource-group <resource-group> --name <vm>
az vm identity show --resource-group <resource-group> --name <vm> --query principalId --output tsv
```

**User-assigned identity**: create it, then attach it to the machines that run Terraform:

```bash
az identity create --resource-group <resource-group> --name terraform-msteams
az vm identity assign --resource-group <resource-group> --name <vm> \
  --identities terraform-msteams
az identity show --resource-group <resource-group> --name terraform-msteams \
  --query "{clientId: clientId, objectId: principalId}"
```

**Azure Arc-enabled server**: connecting the server to Arc creates its system-assigned identity, named after the server. Arc protects the token with a file that only privileged accounts can read: add the account that runs Terraform to the `himds` group (Linux) or to the local **Hybrid agent extension applications** group (Windows), never to the administrators. The membership applies to new sessions: restart the CI runner service.

```powershell
Add-LocalGroupMember -Group "Hybrid agent extension applications" -Member "<DOMAIN>\<account>"
```

## Step 2 — Grant Organization.Read.All

The Entra admin center cannot grant an application permission to a managed identity: use Microsoft Graph PowerShell, as a Global Administrator or Privileged Role Administrator.

```powershell
Connect-MgGraph -Scopes "AppRoleAssignment.ReadWrite.All", "Application.Read.All"

# The Object ID of the managed identity (step 1), not its client ID
$identity = "<object-id>"

# Microsoft Graph and its Organization.Read.All application permission, the same in every tenant
$graph = Get-MgServicePrincipal -Filter "appId eq '00000003-0000-0000-c000-000000000000'"
New-MgServicePrincipalAppRoleAssignment -ServicePrincipalId $identity `
    -PrincipalId $identity `
    -ResourceId $graph.Id `
    -AppRoleId "498476ce-e0fe-48b0-b801-37ba7e2685c6"
```

## Step 3 — Assign the Teams Administrator Role

In **Roles and administrators → Teams Administrator → Add assignments**, search for the identity by name (the VM, the server, or the user-assigned identity). With the Azure CLI (`69091246-20e8-4a56-aa4d-066075b2a7a8` is Teams Administrator in every tenant):

```bash
az rest --method POST \
  --uri "https://graph.microsoft.com/v1.0/roleManagement/directory/roleAssignments" \
  --body '{"principalId": "<object-id>", "directoryScopeId": "/",
           "roleDefinitionId": "69091246-20e8-4a56-aa4d-066075b2a7a8"}'
```

For `terraform plan` and drift detection, a separate identity with the **Teams Reader** role is enough: see the read and write split in the [Authentication guide](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/authentication).

**Grant the permissions before Terraform first runs on the machine.** Azure caches the tokens of a managed identity for up to 24 hours: a token obtained before the role applied keeps lacking it until it expires. To check the Graph token from the machine itself (this prints the token's claims, never the token):

```bash
curl -s -H Metadata:true "http://169.254.169.254/metadata/identity/oauth2/token\
?api-version=2018-02-01&resource=https://graph.microsoft.com" \
  | jq -r '.access_token | split(".")[1] | gsub("-"; "+") | gsub("_"; "/") | @base64d' \
  | jq '{appid, roles, wids}'
```

`roles` must contain `Organization.Read.All`, and `wids` the Teams Administrator ID. For a user-assigned identity, add `&client_id=<client-id>` to the URL.

## Step 4 — Configure the Provider

```terraform
provider "msteams" {
  auth_method = "managed_identity"
  tenant_id   = "contoso.onmicrosoft.com"
  client_id   = "<client-id-of-the-identity>" # user-assigned identity only
}
```

Or with environment variables: `TEAMS_AUTH_METHOD=managed_identity`, `TEAMS_TENANT_ID`, and `TEAMS_CLIENT_ID` for a user-assigned identity only. For the system-assigned identity, make sure `TEAMS_CLIENT_ID` is not left over from a certificate configuration on the same runner: the provider would ask for a user-assigned identity with that ID.

Managed identities are supported in the `global` and `gcc` environments; use a certificate in the other clouds.

## Security Best Practices

- **Everything on the machine can use its identity**: any process, job or user that runs there obtains a token. Run Terraform on machines dedicated to it.
- **On Azure Arc**, keep the `himds` group or the Hybrid agent extension applications group limited to the accounts that run Terraform.
- **One identity per role**: a read identity for plans, a write identity for applies.

## Troubleshooting

Every error from the identity endpoint starts with `obtaining the Microsoft Teams access tokens with the managed identity failed`, followed by the reason given by Azure.

| Error or symptom | Cause | Fix |
|---|---|---|
| `Get "http://169.254.169.254/...": context deadline exceeded`, after less than a minute | Terraform does not run on an Azure resource with a managed identity | Run it on an Azure resource or an Arc-enabled server, or use a certificate |
| `Identity not found`, or an HTTP 400 that names the client ID | The user-assigned identity is not attached, `client_id` holds another ID, or a leftover `TEAMS_CLIENT_ID` is set | Attach the identity and set its **client ID**, or unset `TEAMS_CLIENT_ID` for the system-assigned identity |
| Azure Arc: an error about reading a file under the agent's `tokens` folder | The account running Terraform is not in the `himds` or Hybrid agent extension applications group | Add it, then restart the runner service |
| Azure Arc with `client_id` set: `expected a 401 response, received 404` | Arc only offers the system-assigned identity | Remove `client_id` (and `TEAMS_CLIENT_ID`) |
| `Invalid client_id ... is not a GUID` or `Unknown client_id` at plan time | `client_id` holds a name, or comes from a resource not created yet | Pass the identity's client ID as a known value |
| Connected, but cmdlets fail with access denied | The Teams Administrator role or `Organization.Read.All` is missing, or was granted after the first token | Check steps 2 and 3 and the token's claims; a cached token can lack them for up to 24 hours |

More errors are listed in the [Troubleshooting guide](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/troubleshooting).
