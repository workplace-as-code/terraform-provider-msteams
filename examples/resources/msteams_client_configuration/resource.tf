# Tenant-wide Teams client settings. Only the attributes listed here are
# managed; the others keep their current value.
resource "msteams_client_configuration" "this" {
  allow_drop_box     = false
  allow_box          = false
  allow_google_drive = false
  allow_share_file   = false
  allow_egnyte       = false

  allow_email_into_channel = true
  restricted_sender_list   = "contoso.com;fabrikam.com"
}
