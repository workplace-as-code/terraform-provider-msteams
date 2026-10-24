# Tenant-wide meeting settings: block anonymous join and pin media ports so the
# network team can apply QoS rules.
resource "msteams_meeting_configuration" "this" {
  disable_anonymous_join = true

  enable_qos                      = true
  client_media_port_range_enabled = true
  client_audio_port               = 50000
  client_audio_port_range         = 20
  client_video_port               = 50020
  client_video_port_range         = 20
  client_app_sharing_port         = 50040
  client_app_sharing_port_range   = 20

  legal_url = "https://contoso.com/legal/meetings"
  help_url  = "https://support.contoso.com/teams"
}
