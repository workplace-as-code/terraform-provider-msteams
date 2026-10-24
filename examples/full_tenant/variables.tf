variable "tenant_id" {
  description = "The Entra ID tenant ID or .onmicrosoft.com domain"
  type        = string
}

variable "client_id" {
  description = "The app registration client ID"
  type        = string
}

variable "certificate_path" {
  description = "Path to the .pfx certificate file used for app-only authentication"
  type        = string
}

variable "certificate_password" {
  description = "Password for the .pfx certificate file"
  type        = string
  sensitive   = true
}
