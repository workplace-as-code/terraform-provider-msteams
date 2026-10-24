---
page_title: "Import an existing tenant"
subcategory: "Guides"
---

# Import an existing tenant

A tenant in production already has meeting, messaging and calling policies and tenant settings. Terraform 1.14 and later can find them through the provider and write their configuration for you, so you start from what is configured today instead of rewriting it by hand.

Every resource has a **list resource** of the same name that returns the existing objects of that type. `terraform query` runs the list resources, and `-generate-config-out` turns the results into resource and `import` blocks.

## 1. List the objects

Next to your provider configuration, create a file whose name ends in `.tfquery.hcl`, with one `list` block per resource type to import:

```terraform
# export.tfquery.hcl
list "msteams_meeting_policy" "all" {
  provider = msteams
}

list "msteams_messaging_policy" "all" {
  provider = msteams
}

list "msteams_client_configuration" "all" {
  provider = msteams
}
```

A `list` block takes no arguments. A policy list returns every policy of that type: the org-wide policy (`Global`), the custom policies and the built-in ones (`AllOn`, `Kiosk`, `RestrictedAnonymousAccess`, ...). A configuration list returns its single `Global` object.

```shell
terraform init
terraform query
```

`terraform query` prints the objects found. It reads the tenant and changes nothing.

## 2. Generate the configuration

```shell
terraform query -generate-config-out=generated.tf
```

`generated.tf` holds a `resource` block with the current values and an `import` block for each object.

## 3. Review, then import

```shell
terraform plan
```

The plan must only show imports: `Plan: N to import, 0 to add, 0 to change, 0 to destroy.` `terraform apply` then records the objects in the state, without changing Teams.

Review `generated.tf` before applying:

- **Remove what Terraform should not manage.** Built-in policies cannot be deleted: `terraform destroy` fails on them. Keep them only if you want to manage their settings, ideally with `lifecycle { prevent_destroy = true }`. The `Global` policies and the configurations are never deleted: destroying them only removes them from the state.
- **Remove the attributes you do not want to manage.** The generated blocks set every attribute, and every resource only manages the attributes present in the configuration: deleting a line leaves that setting to the Teams admin center.
- **Rename the resources.** Terraform names them `<list name>_<n>`. Rename them in the `resource` block and in the `to` of the `import` block before applying, or with `moved` blocks afterwards.

## Import a single object

An `import` block takes the object's ID or its identity (Terraform 1.12 and later):

```terraform
import {
  to       = msteams_meeting_policy.external_speakers
  identity = { id = "External speakers" }
}
```

`terraform plan -generate-config-out=generated.tf` writes the configuration of the objects in `import` blocks that have no `resource` block yet. Each resource page lists its import ID.
