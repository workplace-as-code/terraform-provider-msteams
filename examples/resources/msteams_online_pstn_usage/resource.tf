# Usages link the voice routes to the voice routing policies.
resource "msteams_online_pstn_usage" "france" {
  name = "France"
}

resource "msteams_online_pstn_usage" "international" {
  name = "International"
}
