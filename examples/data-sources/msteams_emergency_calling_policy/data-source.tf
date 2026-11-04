data "msteams_emergency_calling_policy" "global" {
  name = "Global"
}

output "global_notification_mode" {
  description = "Notification mode of the org-wide policy."
  value       = data.msteams_emergency_calling_policy.global.notification_mode
}
