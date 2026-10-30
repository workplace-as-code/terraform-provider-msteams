# The org-wide default policy, which applies to every user without another one.
data "msteams_calling_policy" "global" {
  name = "Global"
}

output "allow_private_calling" {
  description = "Whether users can make private calls, by default."
  value       = data.msteams_calling_policy.global.allow_private_calling
}
