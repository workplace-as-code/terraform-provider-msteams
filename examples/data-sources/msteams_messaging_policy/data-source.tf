# The org-wide default policy, which applies to every user without another one.
data "msteams_messaging_policy" "global" {
  name = "Global"
}

output "allow_user_delete_message" {
  description = "Whether users can delete the messages they sent, by default."
  value       = data.msteams_messaging_policy.global.allow_user_delete_message
}
