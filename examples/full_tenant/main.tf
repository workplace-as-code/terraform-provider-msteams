terraform {
  required_providers {
    msteams = {
      source  = "workplace-as-code/msteams"
      version = "~> 0.1"
    }
  }
}

provider "msteams" {
  tenant_id            = var.tenant_id
  client_id            = var.client_id
  certificate_path     = var.certificate_path
  certificate_password = var.certificate_password
}

# Baseline Teams configuration for a tenant.

resource "msteams_client_configuration" "baseline" {
  allow_drop_box     = false
  allow_box          = false
  allow_google_drive = false
  allow_share_file   = false
  allow_egnyte       = false
  allow_guest_user   = true
}

resource "msteams_meeting_configuration" "baseline" {
  disable_anonymous_join = true
  enable_qos             = true
}

resource "msteams_messaging_policy" "restricted" {
  name                       = "Restricted"
  description                = "Users under legal hold or in regulated roles"
  allow_user_delete_message  = false
  allow_user_edit_message    = false
  allow_user_delete_chat     = false
  allow_giphy                = false
  allow_memes                = false
  read_receipts_enabled_type = "Everyone"
}
