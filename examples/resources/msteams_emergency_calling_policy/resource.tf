# The Paris security desk is told of every emergency call.
resource "msteams_emergency_calling_policy" "paris" {
  name                          = "Paris"
  notification_mode             = "NotificationOnly"
  notification_group            = "security-paris@contoso.com"
  external_location_lookup_mode = "Enabled"
}
