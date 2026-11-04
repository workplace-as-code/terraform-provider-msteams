data "msteams_emergency_call_routing_policy" "global" {
  name = "Global"
}

output "global_emergency_numbers" {
  description = "Emergency numbers of the org-wide policy."
  value       = data.msteams_emergency_call_routing_policy.global.emergency_numbers
}
