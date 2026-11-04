# Paris office: 4-digit extensions and 0 for the switchboard.
resource "msteams_tenant_dial_plan" "paris" {
  name        = "Paris"
  description = "Paris office"

  normalization_rules = [
    {
      name                  = "Extensions"
      pattern               = "^(\\d{4})$"
      translation           = "+3314567$1"
      is_internal_extension = true
    },
    {
      name        = "Switchboard"
      pattern     = "^0$"
      translation = "+33145670000"
    },
  ]
}
