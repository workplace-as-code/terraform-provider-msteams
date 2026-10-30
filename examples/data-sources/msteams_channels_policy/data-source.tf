# The org-wide default policy, which applies to every user without another one.
data "msteams_channels_policy" "global" {
  name = "Global"
}

output "allow_private_channel_creation" {
  description = "Whether users can create private channels, by default."
  value       = data.msteams_channels_policy.global.allow_private_channel_creation
}
