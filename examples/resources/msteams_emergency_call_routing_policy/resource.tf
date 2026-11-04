resource "msteams_online_pstn_usage" "emergency" {
  name = "Emergency France"
}

# 112, and the numbers users may dial for it, through the emergency routes.
resource "msteams_emergency_call_routing_policy" "france" {
  name                              = "France"
  allow_enhanced_emergency_services = true

  emergency_numbers = [
    {
      dial_string = "112"
      dial_mask   = "15;17;18"
      pstn_usage  = msteams_online_pstn_usage.emergency.name
    },
  ]
}
