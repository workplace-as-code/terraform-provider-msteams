# External access only with two partner organizations, and no unmanaged
# (personal) Teams accounts.
resource "msteams_tenant_federation_configuration" "this" {
  allow_federated_users = true
  allowed_domains       = ["fabrikam.com", "contoso.com"]

  allow_teams_consumer         = false
  allow_teams_consumer_inbound = false
}
