# No forwarding to external numbers, voicemail always on.
resource "msteams_calling_policy" "restricted" {
  name        = "Restricted-Calling"
  description = "Internal calling only"

  allow_private_calling          = true
  allow_call_forwarding_to_user  = true
  allow_call_forwarding_to_phone = false
  allow_voicemail                = "AlwaysEnabled"
  busy_on_busy_enabled_type      = "Enabled"
}
