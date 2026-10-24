# Users cannot upload custom apps or rearrange the app bar.
resource "msteams_app_setup_policy" "locked" {
  name               = "Locked-AppBar"
  description        = "Fixed app bar, no custom app upload"
  allow_user_pinning = false
  allow_side_loading = false
}
