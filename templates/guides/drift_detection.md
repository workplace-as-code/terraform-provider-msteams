---
page_title: "Detect drift in CI"
subcategory: "Guides"
---

# Detect drift in CI

Teams settings also change outside Terraform: in the Teams admin center, with PowerShell, or through another tool. Such a change is a **drift**: the tenant no longer matches your configuration, and the next `terraform apply` would silently revert it.

A scheduled `terraform plan` finds drifts early. It reads every policy and setting your configuration manages, compares them with the code, and fails when they differ. It never changes the tenant.

## How it works

`terraform plan -detailed-exitcode` tells the result through its exit code:

| Exit code | Meaning | Job result |
|---|---|---|
| `0` | The tenant matches the configuration | Success |
| `1` | The plan failed (authentication, permissions, network...) | Failure |
| `2` | The tenant differs from the configuration | Failure: someone needs to look |

Run it on a schedule, on the branch you deploy from, with the same state backend and credentials as your deployment pipeline. Your CI then notifies you of a failed run like any other failed job.

## GitHub Actions

```yaml
# .github/workflows/drift.yml
name: Drift detection

on:
  schedule:
    - cron: "0 5 * * 1-5" # every working day at 05:00 UTC
  workflow_dispatch:

permissions:
  contents: read

jobs:
  drift:
    runs-on: ubuntu-latest
    # An environment restricted to the default branch keeps the tenant
    # credentials away from pull requests.
    environment: production
    steps:
      - uses: actions/checkout@v7
      - uses: hashicorp/setup-terraform@v4
        with:
          terraform_wrapper: false

      - name: Install the MicrosoftTeams module
        shell: pwsh
        run: Install-PSResource -Name MicrosoftTeams -Scope CurrentUser -TrustRepository -Quiet

      - name: Write the certificate
        env:
          PFX_B64: ${{ secrets.TEAMS_CERTIFICATE_PFX_B64 }}
        run: |
          umask 077
          echo "$PFX_B64" | base64 -d > "$RUNNER_TEMP/teams.pfx"

      - name: Plan
        env:
          TEAMS_TENANT_ID: ${{ secrets.TEAMS_TENANT_ID }}
          TEAMS_CLIENT_ID: ${{ secrets.TEAMS_CLIENT_ID }}
          TEAMS_CERTIFICATE_PATH: ${{ runner.temp }}/teams.pfx
          TEAMS_CERTIFICATE_PASSWORD: ${{ secrets.TEAMS_CERTIFICATE_PASSWORD }}
          # Add the credentials of your state backend here.
        run: |
          terraform init -input=false
          terraform plan -detailed-exitcode -input=false -no-color

      - name: Remove the certificate
        if: always()
        run: rm -f "$RUNNER_TEMP/teams.pfx"
```

Store the certificate as a base64 secret (`base64 -i certificate.pfx`), never in the repository. In production, pin the actions to a commit SHA instead of a tag.

## GitLab CI

Add a job that only runs in scheduled pipelines, next to your `plan` and `apply` jobs:

```yaml
drift:
  stage: plan
  rules:
    - if: $CI_PIPELINE_SOURCE == "schedule"
  resource_group: teams-tenant # never at the same time as an apply
  variables:
    TEAMS_CERTIFICATE_PATH: "/tmp/teams-${CI_JOB_ID}.pfx"
  before_script:
    - umask 077
    - echo "$TEAMS_CERTIFICATE_PFX_B64" | base64 -d > "$TEAMS_CERTIFICATE_PATH"
    - terraform init -input=false
  script:
    - terraform plan -detailed-exitcode -input=false
  after_script:
    - rm -f "$TEAMS_CERTIFICATE_PATH"
```

`TEAMS_TENANT_ID`, `TEAMS_CLIENT_ID`, `TEAMS_CERTIFICATE_PASSWORD` and `TEAMS_CERTIFICATE_PFX_B64` are CI/CD variables of the project (masked; protected if only protected branches run this job). With the GitLab-managed Terraform state, the `TF_HTTP_*` variables of your pipeline configure the backend.

Then create the schedule in **Build > Pipeline schedules**, for example with the interval `0 5 * * 1-5` on the default branch, and run it once with the play button to check it.

## Read the result

A drift appears in the plan as a change of the attribute that was modified outside Terraform:

```text
  # msteams_messaging_policy.students will be updated in-place
  ~ resource "msteams_messaging_policy" "students" {
      ~ allow_giphy = true -> false
        id          = "Students"
        name        = "Students"
        # (20 unchanged attributes hidden)
    }

Plan: 0 to add, 1 to change, 0 to destroy.
```

The value on the left is the one in Teams today, the value on the right is the one in your configuration: someone enabled Giphy in the admin center, and the next apply would disable it again.

A policy deleted outside Terraform appears as `will be created`.

## Resolve a drift

Pick one of three answers:

1. **The configuration is right**: the change was a mistake. Run your deployment pipeline: `apply` sets the value back.
2. **The tenant is right**: the change was intended. Update the configuration to the new value through a merge request, so that the code describes the tenant again.
3. **This setting should not be managed by Terraform**: remove the attribute from the configuration. The provider only manages the attributes set in the configuration, so an attribute left out is never written and no longer appears in the plan: the admins can change it freely.

## Limits

- The plan only checks what the configuration manages. A new policy created in the admin center is not a drift: run [`terraform query`](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/import_existing) from time to time to find objects that are not managed yet.
- The plan reads every managed object: with many objects, expect a few minutes per run, and schedule it outside your deployment hours.
- The drift job needs the same permissions as your deployments: see the [Authentication guide](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/authentication).
