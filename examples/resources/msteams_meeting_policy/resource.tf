# Meetings for external-facing staff: guests wait in the lobby, recordings
# expire after 60 days.
resource "msteams_meeting_policy" "external" {
  name        = "External-Meetings"
  description = "Meetings with customers and partners"

  auto_admitted_users                   = "EveryoneInCompanyExcludingGuests"
  allow_pstn_users_to_bypass_lobby      = false
  allow_anonymous_users_to_join_meeting = true
  allow_cloud_recording                 = true
  new_meeting_recording_expiration_days = 60
  allow_transcription                   = true
  screen_sharing_mode                   = "SingleApplication"
}

# The org-wide default policy is adopted, not created: destroying this
# resource only removes it from Terraform state.
resource "msteams_meeting_policy" "global" {
  name                    = "Global"
  allow_breakout_rooms    = true
  allow_meeting_reactions = true
}
