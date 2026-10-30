# The org-wide default policy, which applies to every user without another one.
data "msteams_feedback_policy" "global" {
  name = "Global"
}

output "user_initiated_mode" {
  description = "Whether users can send feedback to Microsoft, by default."
  value       = data.msteams_feedback_policy.global.user_initiated_mode
}
