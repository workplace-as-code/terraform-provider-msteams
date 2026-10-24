# Pilot users get the public preview of the Teams client.
resource "msteams_update_management_policy" "pilot" {
  name                 = "Pilot-Preview"
  description          = "Early access to Teams features"
  allow_public_preview = "Enabled"
}
