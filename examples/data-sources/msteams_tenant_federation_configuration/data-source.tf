data "msteams_tenant_federation_configuration" "this" {}

output "allowed_domains" {
  description = "Domains whose users can chat with the tenant."
  value       = data.msteams_tenant_federation_configuration.this.allowed_domains
}
