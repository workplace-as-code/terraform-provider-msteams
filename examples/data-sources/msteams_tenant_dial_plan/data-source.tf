data "msteams_tenant_dial_plan" "global" {
  name = "Global"
}

output "global_normalization_rules" {
  description = "Normalization rules of the org-wide dial plan."
  value       = data.msteams_tenant_dial_plan.global.normalization_rules
}
