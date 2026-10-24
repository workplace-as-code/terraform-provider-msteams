# Restrictive messaging for students: no Giphy, memes or message deletion.
resource "msteams_messaging_policy" "students" {
  name        = "Students"
  description = "Messaging policy for student accounts"

  allow_user_delete_message  = false
  allow_owner_delete_message = true
  allow_giphy                = false
  allow_memes                = false
  allow_stickers             = true
  read_receipts_enabled_type = "Everyone"
}

# Frontline workers: chat allowed, audio messages in chats only.
resource "msteams_messaging_policy" "frontline" {
  name                       = "Frontline"
  audio_message_enabled_type = "ChatsOnly"
  allow_priority_messages    = true
}
