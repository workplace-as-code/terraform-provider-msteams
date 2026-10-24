# Guests can use video but only share a single application.
resource "msteams_guest_meeting_configuration" "this" {
  allow_ip_video      = true
  screen_sharing_mode = "SingleApplication"
  allow_meet_now      = false
}
