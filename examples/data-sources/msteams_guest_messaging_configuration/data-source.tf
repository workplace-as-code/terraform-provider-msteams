data "msteams_guest_messaging_configuration" "this" {}

output "allow_giphy" {
  description = "Whether guests can use Giphy in chats."
  value       = data.msteams_guest_messaging_configuration.this.allow_giphy
}
