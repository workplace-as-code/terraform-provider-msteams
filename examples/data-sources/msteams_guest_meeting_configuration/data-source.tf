data "msteams_guest_meeting_configuration" "this" {}

output "allow_meet_now" {
  description = "Whether guests can start instant meetings."
  value       = data.msteams_guest_meeting_configuration.this.allow_meet_now
}
