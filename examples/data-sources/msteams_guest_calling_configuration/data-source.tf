data "msteams_guest_calling_configuration" "this" {}

output "allow_private_calling" {
  description = "Whether guests can make private calls."
  value       = data.msteams_guest_calling_configuration.this.allow_private_calling
}
