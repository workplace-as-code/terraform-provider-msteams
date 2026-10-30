# The org-wide default policy, which applies to every user without another one.
data "msteams_meeting_policy" "global" {
  name = "Global"
}

output "allow_meet_now" {
  description = "Whether users can start instant meetings, by default."
  value       = data.msteams_meeting_policy.global.allow_meet_now
}
