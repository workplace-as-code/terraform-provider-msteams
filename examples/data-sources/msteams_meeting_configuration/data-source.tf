data "msteams_meeting_configuration" "this" {}

output "disable_anonymous_join" {
  description = "Whether anonymous users are blocked from joining meetings."
  value       = data.msteams_meeting_configuration.this.disable_anonymous_join
}
