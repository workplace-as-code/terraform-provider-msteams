data "msteams_client_configuration" "this" {}

output "allow_drop_box" {
  description = "Whether Dropbox can be used for file storage in Teams."
  value       = data.msteams_client_configuration.this.allow_drop_box
}
