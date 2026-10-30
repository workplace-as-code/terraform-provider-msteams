# The org-wide default policy, which applies to every user without another one.
data "msteams_app_setup_policy" "global" {
  name = "Global"
}

output "allow_side_loading" {
  description = "Whether users can upload custom apps, by default."
  value       = data.msteams_app_setup_policy.global.allow_side_loading
}
