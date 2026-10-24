---
page_title: "Getting Started"
subcategory: "Guides"
---

# Getting Started

This guide takes you from an empty folder to a Teams messaging policy managed by Terraform.

## Prerequisites

- Terraform 1.5 or later
- PowerShell 7+ (`pwsh --version`)
- The MicrosoftTeams module: `pwsh -Command "Install-Module MicrosoftTeams -Scope CurrentUser"`
- An app registration with a certificate and the Teams Administrator role: see the [Authentication guide](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/authentication)

## Step 1 — Configure the Provider

```terraform
# providers.tf
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
```

Declare the variables used by the provider block:

```terraform
# variables.tf
variable "tenant_id" {
  type = string
}

variable "client_id" {
  type = string
}

variable "certificate_path" {
  type = string
}

variable "certificate_password" {
  type      = string
  sensitive = true
}
```

Give them values in a `terraform.tfvars` file, or with `TF_VAR_<name>` environment variables. Keep `terraform.tfvars` out of version control: it holds the certificate password.

```terraform
# terraform.tfvars
tenant_id            = "contoso.onmicrosoft.com"
client_id            = "00000000-0000-0000-0000-000000000000"
certificate_path     = "/path/to/terraform-msteams.pfx"
certificate_password = "your-cert-password"
```

Every argument can also come from a `TEAMS_*` environment variable, for example `TEAMS_TENANT_ID`, with an empty provider block: `provider "msteams" {}`.

## Step 2 — Bring Existing Settings Under Management

Tenant-wide settings already exist, so import them before changing them. Import blocks let `terraform plan` show the current values first:

```terraform
# main.tf
import {
  to = msteams_meeting_configuration.this
  id = "Global"
}

resource "msteams_meeting_configuration" "this" {
  disable_anonymous_join = true
}
```

Only the attributes you write are managed. The others keep their current Teams value and are recorded in state.

Tenant-wide settings cannot be deleted: `terraform destroy` only removes them from the state, and they keep the values Terraform wrote. To undo the change, set `disable_anonymous_join` back to its previous value and apply before destroying.

## Step 3 — Create a Policy

```terraform
resource "msteams_messaging_policy" "students" {
  name        = "Students"
  description = "Messaging policy for student accounts"
  allow_giphy = false
  allow_memes = false
}
```

## Step 4 — Plan and Apply

```bash
terraform init
terraform plan
terraform apply
```

The first operation takes longer because the provider loads the MicrosoftTeams module and connects. The following ones reuse the open PowerShell sessions.

## Next Steps

- [Import an existing tenant](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/import_existing): generate the configuration of the policies and settings already in place.
- Assign policies to groups with `msteams_group_policy_assignment`. Assigning policies to individual users is not managed by the provider yet.
- Run `terraform plan` on a schedule to detect changes made in the Teams admin center.
