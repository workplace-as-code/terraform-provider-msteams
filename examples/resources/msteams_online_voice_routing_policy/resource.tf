resource "msteams_online_pstn_usage" "france" {
  name = "France"
}

resource "msteams_online_pstn_usage" "international" {
  name = "International"
}

# Users of this policy call French numbers first through the France routes.
resource "msteams_online_voice_routing_policy" "france" {
  name = "France"
  online_pstn_usages = [
    msteams_online_pstn_usage.france.name,
    msteams_online_pstn_usage.international.name,
  ]
}
