---
page_title: "Troubleshooting"
subcategory: "Guides"
---

# Troubleshooting

The provider runs PowerShell scripts that call the MicrosoftTeams module. When something fails, find the layer first: installing the provider, starting PowerShell, connecting to Teams, permissions, or the Teams operation itself. The sections below follow that order.

## Read the Error

When a Teams cmdlet fails, the error ends with the PowerShell message, unchanged:

```text
PowerShell script cs_object.ps1 failed: <message of the failing cmdlet>
```

The script name tells which operation failed: `cs_object.ps1` reads and writes policies and configurations, `group_policy_assignment.ps1` manages group policy assignments, `list.ps1` lists objects for `terraform query`. The message after `failed:` comes from Teams or from PowerShell: search for it as is.

## Provider Installation

**`no available releases match the given constraints`, although the version exists on the registry.** A local copy of the provider hides the registry. Terraform looks for providers in local plugin directories first (`~/.terraform.d/plugins`, `~/.local/share/terraform/plugins`, `/usr/local/share/terraform/plugins`, `/usr/share/terraform/plugins`, on macOS `~/Library/Application Support/io.terraform/plugins`, on Windows `%APPDATA%\terraform.d\plugins` and `%APPDATA%\HashiCorp\Terraform\plugins`). When it finds a provider there, it installs that provider only from there. Remove the `workplace-as-code` folder from these directories, or declare an explicit `provider_installation` block in the CLI configuration.

**`Provider development overrides are in effect`.** The CLI configuration (`~/.terraformrc`, or the file named by `TF_CLI_CONFIG_FILE`) has a `dev_overrides` entry for the provider: Terraform runs a local build instead of the published version. Remove the entry to use the registry.

**Checksum errors on another machine** (`does not match any of the checksums recorded in the dependency lock file`). The lock file only lists the platforms it was created on. Record every platform that runs Terraform:

```bash
terraform providers lock -platform=linux_amd64 -platform=darwin_arm64 -platform=windows_amd64
```

**No Internet access.** The registry sends Terraform to GitHub to download the release files. Without access to both, serve the provider from a network mirror (`terraform providers mirror`, or a repository manager such as Artifactory) and point the CLI configuration at it.

## PowerShell and the MicrosoftTeams Module

**`the Microsoft Teams provider requires PowerShell 7+ (pwsh)`.** Install [PowerShell 7](https://aka.ms/install-powershell). `pwsh` must be on the `PATH` of the account that runs Terraform: check it in the CI job or service, not only in your own shell.

**`Failed to connect to Microsoft Teams: The term 'Connect-MicrosoftTeams' is not recognized`.** The module is not installed for the account that runs Terraform. The provider starts PowerShell with `-NoProfile`, so a module loaded from a profile is not found either. Install it for that account:

```powershell
Install-PSResource -Name MicrosoftTeams -Scope CurrentUser -TrustRepository
```

Check which versions PowerShell finds, and where:

```powershell
Get-Module -ListAvailable MicrosoftTeams | Select-Object Name, Version, ModuleBase
```

When several versions are installed, PowerShell loads the newest one. The GCC High and DoD clouds need version 5.0.1 or later. The tested versions are listed on the [provider page](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs).

**`PowerShell bridge host failed to start`, `did not start within`, or `exited while running`.** The provider keeps a few PowerShell processes open and sends them requests. To get the error of a single call, closer to the failing cmdlet, run once with one process per call:

```bash
TEAMS_BRIDGE_MODE=oneshot terraform plan
```

**PowerShell crashes when many processes start at once.** PowerShell keeps a startup cache that a burst of parallel starts can corrupt; every later start then fails. Delete it, it is rebuilt automatically:

- Linux and macOS: `~/.cache/powershell/StartupProfileData-NonInteractive`
- Windows: `%LOCALAPPDATA%\Microsoft\PowerShell\StartupProfileData-NonInteractive`

## Authentication

Connection errors start with `Failed to connect to Microsoft Teams:`, followed by the Entra ID message. The `AADSTS` code tells what is wrong:

| Code | Meaning | Fix |
|---|---|---|
| `AADSTS700016` | No application with this client ID in the tenant | Check `client_id`, and that the app registration is in the tenant named by `tenant_id` |
| `AADSTS90002` | Tenant not found | Check `tenant_id`: the directory ID or the `.onmicrosoft.com` domain |
| `AADSTS700027` | The certificate is not registered on the application, or has expired | Upload the `.cer` of the same certificate as the `.pfx`, and check its end date in **Certificates & secrets** |

**The certificate file cannot be read.** With a `.pfx` file, a wrong password fails before any connection, with a message such as `The certificate data cannot be read with the provided password`. Check `certificate_password`, and give `certificate_path` as an absolute path: CI jobs and services often run in another working directory.

**macOS: `This platform does not support loading with EphemeralKeySet`.** Fixed in version 0.1.1: upgrade the provider.

The steps to create the app registration and the certificate are in the [Authentication guide](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/authentication).

## Permissions

**The connection works, but reads or writes are refused.** Check, in this order:

1. The app has the **Teams Administrator** role: **Entra ID → Roles and administrators → Teams Administrator → Assignments**. A new assignment can take up to 30 minutes to apply.
2. **Admin consent** is granted for the `Organization.Read.All` Graph application permission (**API permissions**, status column).
3. `terraform apply` does not use the read-only app: an app with Teams Reader can plan, but not change the tenant (`Forbidden`).

**`msteams_group_policy_assignment` fails on a group** although the role is assigned. When the Teams Administrator role of the app is scoped to administrative units, the app also needs the Microsoft Graph `GroupMember.Read.All` application permission, with admin consent.

## Timeouts, Conflicts and Throttling

**`PowerShell script <name> timed out after 10m (set TEAMS_SCRIPT_TIMEOUT to raise the limit)`.** One call took longer than the limit. The first call of a run also loads the module and connects. Raise the limit, for example `TEAMS_SCRIPT_TIMEOUT=20m`.

**Write conflicts.** Teams stores all the policies of one type in a single tenant document, so two writes to the same policy type at the same moment conflict. The provider writes one policy of each type at a time and retries a write that conflicts with a change made elsewhere at the same moment (five attempts). If conflicts persist, check that no administrator or other tool changes the same policies during the apply.

**Many objects, slow or throttled runs.** `TEAMS_MAX_CONCURRENCY` (default `4`) limits the number of PowerShell processes, and so the number of simultaneous requests to Teams. Lower it, for example to `2`, if Teams throttles the requests.

## Debug Logging

```bash
TF_LOG_PROVIDER=DEBUG TF_LOG_PATH=terraform.log terraform plan
```

`TF_LOG_PROVIDER` limits the log to the providers; `TF_LOG=DEBUG` adds Terraform itself. The log shows which resource and which operation failed, in order. The PowerShell error is already in the error message.

The log can contain values from your configuration: read it before sharing it. The certificate password is never written to the log or put on a command line: the provider passes it to PowerShell through the process environment.

## Run a Bridge Script by Hand

Running the script that failed, outside Terraform, shows the full PowerShell error and lets you try a fix.

1. Find the scripts. The provider writes them, at each start, to a folder named after their content hash:
   - Linux: `~/.cache/terraform-provider-msteams/<hash>/`
   - macOS: `~/Library/Caches/terraform-provider-msteams/<hash>/`
   - Windows: `%LOCALAPPDATA%\terraform-provider-msteams\<hash>\`

   They are also in the repository, under `bridge/scripts`, at the tag of your provider version.

2. In `pwsh`, from that folder, connect and call the script with the same parameters as the provider:

   ```powershell
   $auth = @{
       TenantId            = "contoso.onmicrosoft.com"
       ClientId            = "00000000-0000-0000-0000-000000000000"
       CertificatePath     = "/secrets/terraform-msteams.pfx"
       CertificatePassword = Read-Host "PFX password" -MaskInput
   }

   # Lists the messaging policies, as terraform query does
   ./list.ps1 @auth -Noun CsTeamsMessagingPolicy

   # Reads one policy, as terraform plan does
   $properties = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes('["AllowGiphy","AllowMemes"]'))
   ./cs_object.ps1 @auth -Action Get -Noun CsTeamsMessagingPolicy -Identity Students -Properties $properties
   ```

   `-Noun` is the noun of the Teams cmdlets of the resource, listed in its documentation (`Get-CsTeamsMessagingPolicy` → `CsTeamsMessagingPolicy`). A read prints one line of JSON; an empty output means that the object does not exist.

3. To test a change to a script, copy the folder, edit the copy and point the provider at it with `TEAMS_BRIDGE_DIR=/path/to/the/copy`. This is for script development only: remove the variable afterwards.

## Report a Problem

If none of this helps, [open an issue](https://github.com/workplace-as-code/terraform-provider-msteams/issues/new/choose) with the provider, Terraform, PowerShell and MicrosoftTeams module versions, the resource configuration and the full error message. Remove tenant names, IDs and certificate details first.
