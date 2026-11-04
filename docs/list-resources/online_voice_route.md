---
page_title: "msteams_online_voice_route (List Resource) - msteams"
subcategory: ""
description: |-
  Lists the existing msteams_online_voice_route objects, for terraform query.
---

# msteams_online_voice_route (List Resource)

Lists the existing objects of this type, built-in ones included, for `terraform query` (Terraform 1.14 and later). The list block takes no arguments.

Each result is an object that the [`msteams_online_voice_route`](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/resources/online_voice_route) resource can import. `terraform query -generate-config-out=generated.tf` writes the resource and `import` blocks of every result: see [Import an existing tenant](https://registry.terraform.io/providers/workplace-as-code/msteams/latest/docs/guides/import_existing).

## Example Usage

```terraform
list "msteams_online_voice_route" "all" {
  provider = msteams
}
```
