# Read-only check of OIDC authentication, run by .github/workflows/oidc.yml.
# terraform query lists several types at once, so several PowerShell processes
# each connect with the access tokens. Nothing is created or changed in the
# tenant.
terraform {
  required_providers {
    msteams = {
      source = "workplace-as-code/msteams"
    }
  }
}

# Authentication comes from the TEAMS_* environment variables of the job.
provider "msteams" {}
