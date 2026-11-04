data "msteams_online_voice_route" "local" {
  name = "LocalRoute"
}

output "local_number_pattern" {
  description = "Numbers matched by the default route."
  value       = data.msteams_online_voice_route.local.number_pattern
}
