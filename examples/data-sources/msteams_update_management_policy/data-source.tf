# The org-wide default policy, which applies to every user without another one.
data "msteams_update_management_policy" "global" {
  name = "Global"
}

output "allow_public_preview" {
  description = "Whether users can turn on the public preview, by default."
  value       = data.msteams_update_management_policy.global.allow_public_preview
}
