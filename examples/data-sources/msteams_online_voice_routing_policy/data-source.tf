data "msteams_online_voice_routing_policy" "global" {
  name = "Global"
}

output "global_online_pstn_usages" {
  description = "PSTN usages of the org-wide policy."
  value       = data.msteams_online_voice_routing_policy.global.online_pstn_usages
}
