---
page_title: "msteams_group_policy_assignment (List Resource) - msteams"
subcategory: ""
description: |-
  Lists the existing msteams_group_policy_assignment objects, for terraform query.
---

# msteams_group_policy_assignment (List Resource)

Lists the existing objects of this type, built-in ones included, for `terraform query` (Terraform 1.14 and later). The list block takes no arguments.

Each result is an object that the [`msteams_group_policy_assignment`](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/resources/group_policy_assignment) resource can import. `terraform query -generate-config-out=generated.tf` writes the resource and `import` blocks of every result: see [Import an existing tenant](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/import_existing).

## Example Usage

```terraform
list "msteams_group_policy_assignment" "all" {
  provider = msteams
}
```
