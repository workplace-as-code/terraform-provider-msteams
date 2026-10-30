# The org-wide default policy, which applies to every user without another one.
data "msteams_events_policy" "global" {
  name = "Global"
}

output "allow_webinars" {
  description = "Whether users can create webinars, by default."
  value       = data.msteams_events_policy.global.allow_webinars
}
