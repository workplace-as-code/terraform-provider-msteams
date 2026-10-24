# No shared channels with other organizations.
resource "msteams_channels_policy" "internal" {
  name        = "Internal-Channels"
  description = "Private channels allowed, no external sharing"

  allow_private_channel_creation                       = true
  allow_shared_channel_creation                        = true
  allow_channel_sharing_to_external_user               = false
  allow_user_to_participate_in_external_shared_channel = false
}
