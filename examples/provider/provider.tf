terraform {
  required_providers {
    msteams = {
      source  = "workplace-as-code/msteams"
      version = "~> 0.1"
    }
  }
}

provider "msteams" {
  tenant_id            = "contoso.onmicrosoft.com"
  client_id            = var.client_id
  certificate_path     = var.certificate_path
  certificate_password = var.certificate_password
}

resource "msteams_messaging_policy" "students" {
  name        = "Students"
  allow_giphy = false
  allow_memes = false
}
