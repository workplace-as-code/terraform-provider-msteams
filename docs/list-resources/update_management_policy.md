---
page_title: "msteams_update_management_policy (List Resource) - msteams"
subcategory: ""
description: |-
  Lists the existing msteams_update_management_policy objects, for terraform query.
---

# msteams_update_management_policy (List Resource)

Lists the existing objects of this type, built-in ones included, for `terraform query` (Terraform 1.14 and later). The list block takes no arguments.

Each result is an object that the [`msteams_update_management_policy`](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/resources/update_management_policy) resource can import. `terraform query -generate-config-out=generated.tf` writes the resource and `import` blocks of every result: see [Import an existing tenant](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/import_existing).

## Example Usage

```terraform
list "msteams_update_management_policy" "all" {
  provider = msteams
}
```
