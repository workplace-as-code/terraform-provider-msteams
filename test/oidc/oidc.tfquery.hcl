list "msteams_meeting_policy" "all" {
  provider = msteams
}

list "msteams_messaging_policy" "all" {
  provider = msteams
}

list "msteams_calling_policy" "all" {
  provider = msteams
}

list "msteams_tenant_federation_configuration" "all" {
  provider = msteams
}

# Voice: the read-only role must also read the voice settings.
list "msteams_tenant_dial_plan" "all" {
  provider = msteams
}

list "msteams_online_pstn_usage" "all" {
  provider = msteams
}

list "msteams_online_voice_route" "all" {
  provider = msteams
}

list "msteams_online_voice_routing_policy" "all" {
  provider = msteams
}

list "msteams_emergency_calling_policy" "all" {
  provider = msteams
}

list "msteams_emergency_call_routing_policy" "all" {
  provider = msteams
}
