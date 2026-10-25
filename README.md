# Terraform Provider for Microsoft Teams

Manage Microsoft Teams policies and tenant-wide settings as code with Terraform.

[![CI](https://github.com/workplace-as-code/terraform-provider-msteams/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/workplace-as-code/terraform-provider-msteams/actions/workflows/ci.yml)
[![Terraform Registry](https://img.shields.io/github/v/release/workplace-as-code/terraform-provider-msteams?label=registry&logo=terraform&color=7B42BC)](https://registry.terraform.io/providers/workplace-as-code/msteams/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

> [!WARNING]
> **Use at your own risk.** This is a community project, maintained on a best-effort basis and provided as is, without warranty (MIT license). Each release is tested by hand on a test tenant before it is published; nothing is tested continuously against Microsoft 365. Run `terraform plan` and try your configuration on a non-production tenant before applying it to production.

- Review every change with `terraform plan` before it reaches the tenant
- Detect changes made in the Teams admin center
- Start from an existing tenant: `terraform query` finds its policies and settings and generates their configuration
- Promote tested settings from a test tenant to production through Git
- Keep a full audit trail in version control

---

## Quick Start

```hcl
terraform {
  required_providers {
    msteams = {
      source  = "workplace-as-code/msteams"
      version = "~> 0.1"
    }
  }
}

provider "msteams" {
  tenant_id            = var.tenant_id
  client_id            = var.client_id
  certificate_path     = var.certificate_path
  certificate_password = var.certificate_password
}

# Block anonymous users from joining meetings
resource "msteams_meeting_configuration" "this" {
  disable_anonymous_join = true
}

# Restrictive messaging for students
resource "msteams_messaging_policy" "students" {
  name        = "Students"
  allow_giphy = false
  allow_memes = false
}

# Applied to every member of the Students group
resource "msteams_group_policy_assignment" "students" {
  group_id    = var.students_group_id
  policy_type = "TeamsMessagingPolicy"
  policy_name = msteams_messaging_policy.students.name
  rank        = 1
}
```

```bash
terraform init
terraform plan   # Preview changes, nothing applied yet
terraform apply  # Apply to your tenant
```

Only the attributes written in the configuration are managed. The others keep their current Teams value, so adopting a resource never resets settings you did not mention. Import the tenant-wide settings first (`import` block with `id = "Global"`) to see their current values in the plan.

---

## Supported Resources

Full reference in [docs/resources](docs/resources/).

| Area | Resources |
|---|---|
| Policies | `msteams_messaging_policy`, `msteams_meeting_policy`, `msteams_calling_policy`, `msteams_app_setup_policy`, `msteams_channels_policy`, `msteams_update_management_policy`, `msteams_feedback_policy`, `msteams_events_policy` |
| Policy assignment | `msteams_group_policy_assignment` |
| External and guest access | `msteams_tenant_federation_configuration`, `msteams_guest_messaging_configuration`, `msteams_guest_meeting_configuration`, `msteams_guest_calling_configuration` |
| Tenant-wide settings | `msteams_client_configuration`, `msteams_meeting_configuration` |

Every policy resource manages either a custom policy or, with `name = "Global"`, the org-wide default policy. Assign custom policies to groups with `msteams_group_policy_assignment`.

### Related providers

- [workplace-as-code/msexchangeonline](https://github.com/workplace-as-code/terraform-provider-msexchangeonline): Exchange Online mail flow, anti-spam, anti-phishing and Defender for Office 365, built on the same PowerShell bridge.

---

## Installation

### Requirements

- Terraform >= 1.5
- PowerShell 7+ (`pwsh`) with the MicrosoftTeams module (`Install-Module MicrosoftTeams`)
- An Entra ID app registration with a certificate, the `Organization.Read.All` Graph permission and the Teams Administrator role (see the [Authentication guide](docs/guides/authentication.md))

The PowerShell bridge scripts are embedded in the provider binary: nothing else needs to be installed. The provider keeps a few PowerShell processes open during a run, so the Teams connection is opened once per process instead of once per resource.

### Performance tuning

| Variable | Default | Effect |
|---|---|---|
| `TEAMS_MAX_CONCURRENCY` | `4` | Maximum number of PowerShell processes running at once |
| `TEAMS_SCRIPT_TIMEOUT` | `10m` | Maximum duration of one bridge call |
| `TEAMS_BRIDGE_MODE` | `persistent` | `oneshot` starts a new PowerShell process for every call |
| `TEAMS_WORKER_MAX_REQUESTS` | `200` | Requests served by a PowerShell process before it is replaced |
| `TEAMS_BRIDGE_DIR` | unset | Run the bridge scripts from this folder instead of the embedded copy (script development only) |

---

## Documentation

- [Getting Started](docs/guides/getting_started.md)
- [Authentication](docs/guides/authentication.md)
- [Import an existing tenant](docs/guides/import_existing.md)
- [Detect drift in CI](docs/guides/drift_detection.md)
- [Troubleshooting](docs/guides/troubleshooting.md)
- [All Resources](docs/resources/)

---

## Contributing

Contributions are welcome. Read the [contributing guide](CONTRIBUTING.md), then open an issue for a [bug](.github/ISSUE_TEMPLATE/bug_report.md), a [feature](.github/ISSUE_TEMPLATE/feature_request.md) or a [new resource](.github/ISSUE_TEMPLATE/new_resource.md).

## License

[MIT](LICENSE)
