resource "msteams_online_pstn_usage" "france" {
  name = "France"
}

# French numbers through the two SBCs of the Paris data centers.
resource "msteams_online_voice_route" "france" {
  name                 = "France"
  number_pattern       = "^\\+33\\d{9}$"
  priority             = 0
  online_pstn_usages   = [msteams_online_pstn_usage.france.name]
  online_pstn_gateways = ["sbc1.contoso.com", "sbc2.contoso.com"]
}
