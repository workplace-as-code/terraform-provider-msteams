# Changelog

From the next version on, the release notes are generated from the pull request titles: see the [GitHub releases](https://github.com/workplace-as-code/terraform-provider-msteams/releases). This file keeps the history up to version 0.1.1.

## 0.1.1 (October 24, 2026)

### Bug fixes

- Certificate authentication with a `.pfx` file works on macOS: loading the certificate failed with "This platform does not support loading with EphemeralKeySet".

## 0.1.0 (October 24, 2026)

First release.

### Resources

- Policies: `msteams_messaging_policy`, `msteams_meeting_policy`, `msteams_calling_policy`, `msteams_app_setup_policy`, `msteams_channels_policy`, `msteams_update_management_policy`, `msteams_feedback_policy`, `msteams_events_policy`. Each manages a custom policy, or the org-wide default policy with `name = "Global"`.
- Policy assignment: `msteams_group_policy_assignment`.
- Tenant-wide configurations: `msteams_client_configuration`, `msteams_meeting_configuration`, `msteams_guest_messaging_configuration`, `msteams_guest_meeting_configuration`, `msteams_guest_calling_configuration`, `msteams_tenant_federation_configuration` (external access).

### Provider

- A `certificate_path` that does not point to a readable file fails at plan time with a clear message.
- Destroying a resource that cannot be deleted (tenant-wide settings and Global policies) shows a warning: the object is only removed from the Terraform state and keeps its current settings.
- Existing configuration can be imported in bulk: every resource has a list resource for `terraform query` (Terraform 1.14+), which finds the existing objects and generates their configuration with `-generate-config-out`. Resources also have a resource identity, so `import` blocks accept `identity = { id = "..." }`.
- App-only certificate authentication (`.pfx` file or certificate thumbprint), in the global, GCC, GCC High and DoD clouds.
- Only the attributes set in the configuration are managed: the others keep their current Teams value, so adopting an existing policy or configuration never resets it.
- Enum values are compared without regard to case, and policies can be imported with or without the `Tag:` prefix shown by `Get-Cs*Policy`.
- Writes to the same kind of policy are serialized, and Teams concurrency conflicts (`409 Item has changed`) are retried.
- The PowerShell scripts are embedded in the provider binary. PowerShell sessions are kept open during a run, with a cap on concurrent sessions and a timeout per call (`TEAMS_MAX_CONCURRENCY`, `TEAMS_SCRIPT_TIMEOUT`, ...).
