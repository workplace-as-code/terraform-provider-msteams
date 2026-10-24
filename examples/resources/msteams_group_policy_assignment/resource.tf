resource "msteams_meeting_policy" "external" {
  name                = "External-Meetings"
  auto_admitted_users = "EveryoneInCompanyExcludingGuests"
}

# Members of the Sales group get the External-Meetings policy. Rank 1 wins
# over the other group assignments of meeting policies.
resource "msteams_group_policy_assignment" "sales_meetings" {
  group_id    = var.sales_group_id
  policy_type = "TeamsMeetingPolicy"
  policy_name = msteams_meeting_policy.external.name
  rank        = 1
}

variable "sales_group_id" {
  description = "Object ID of the Sales group in Entra ID"
  type        = string
}
