# Guests can chat but not delete chats, and see only strict Giphy content.
resource "msteams_guest_messaging_configuration" "this" {
  allow_user_chat        = true
  allow_user_delete_chat = false
  allow_giphy            = true
  giphy_rating_type      = "Strict"
  allow_memes            = false
}
