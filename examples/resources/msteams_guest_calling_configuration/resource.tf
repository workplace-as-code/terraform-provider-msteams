# Guests cannot place 1:1 calls.
resource "msteams_guest_calling_configuration" "this" {
  allow_private_calling = false
}
