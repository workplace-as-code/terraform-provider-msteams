terraform {
  required_providers {
    msteams = {
      source = "workplace-as-code/msteams"
    }
  }
}

# Auth is read from TEAMS_* env vars — no credentials in this file.
provider "msteams" {}

variable "phase" {
  description = "Set to \"update\" by test/run.sh to change one attribute of every test policy in place."
  type        = string
  default     = "create"

  validation {
    condition     = contains(["create", "update"], var.phase)
    error_message = "phase must be \"create\" or \"update\"."
  }
}

variable "guest_allow_memes" {
  description = "Set by test/run.sh to test a write to a tenant-wide configuration, then to restore the original value. null leaves it unmanaged."
  type        = bool
  default     = null
}

variable "federation_blocked_domains" {
  description = "Set by test/run.sh to test the external access domain lists, then to restore them. null leaves them unmanaged."
  type        = list(string)
  default     = null
}

variable "test_notification_group" {
  description = "Email address notified by the test emergency calling policy. Empty leaves notifications off."
  type        = string
  default     = ""
}

variable "test_group_id" {
  description = "Object ID of an existing Entra ID group, to test group policy assignments. Leave empty to skip."
  type        = string
  default     = ""
}

locals {
  updated     = var.phase == "update"
  description = local.updated ? "[TF-CI-TEST] updated" : "[TF-CI-TEST] created"
}

# ---------------------------------------------------------------------------
# Policies: created, updated, imported and destroyed by the test
# ---------------------------------------------------------------------------

resource "msteams_messaging_policy" "test" {
  name                       = "TF-CI-Messaging"
  description                = local.description
  allow_giphy                = local.updated
  giphy_rating_type          = "STRICT"
  allow_memes                = false
  allow_user_delete_message  = false
  read_receipts_enabled_type = "Everyone"
  audio_message_enabled_type = "ChatsOnly"
}

# Only the name is set: every other attribute must keep the Teams default
# and produce no drift.
resource "msteams_messaging_policy" "minimal" {
  name = "TF-CI-MessagingMinimal"
}

resource "msteams_meeting_policy" "test" {
  name                                  = "TF-CI-Meeting"
  description                           = local.description
  auto_admitted_users                   = local.updated ? "EveryoneInCompanyExcludingGuests" : "EveryoneInCompany"
  allow_cloud_recording                 = true
  allow_transcription                   = !local.updated
  screen_sharing_mode                   = "SingleApplication"
  new_meeting_recording_expiration_days = 60
  live_captions_enabled_type            = "DisabledUserOverride"
}

resource "msteams_calling_policy" "test" {
  name                           = "TF-CI-Calling"
  description                    = local.description
  allow_voicemail                = local.updated ? "AlwaysEnabled" : "UserOverride"
  allow_call_forwarding_to_phone = false
  busy_on_busy_enabled_type      = "Disabled"
}

resource "msteams_app_setup_policy" "test" {
  name               = "TF-CI-AppSetup"
  description        = local.description
  allow_user_pinning = !local.updated
  allow_side_loading = false
}

resource "msteams_channels_policy" "test" {
  name                           = "TF-CI-Channels"
  description                    = local.description
  allow_private_channel_creation = !local.updated
  allow_shared_channel_creation  = false
}

resource "msteams_update_management_policy" "test" {
  name                 = "TF-CI-Updates"
  description          = local.description
  allow_public_preview = local.updated ? "Disabled" : "FollowOfficePreview"
}

resource "msteams_feedback_policy" "test" {
  name                        = "TF-CI-Feedback"
  user_initiated_mode         = local.updated ? "Disabled" : "Enabled"
  allow_screenshot_collection = false
}

resource "msteams_events_policy" "test" {
  name            = "TF-CI-Events"
  description     = local.description
  allow_webinars  = local.updated ? "Disabled" : "Enabled"
  allow_townhalls = "Enabled"
}

# The org-wide default policy is adopted, not created. No attribute is set,
# so Terraform only records it, and destroying it only removes it from state.
resource "msteams_feedback_policy" "global" {
  name = "Global"
}

# ---------------------------------------------------------------------------
# Voice. The test tenant has no session border controller (SBC): the routes
# carry no call. The update phase reorders the PSTN usages of the voice
# routing policy, whose order matters.
# ---------------------------------------------------------------------------

resource "msteams_online_pstn_usage" "test" {
  name = "TF-CI-Usage"
}

resource "msteams_online_pstn_usage" "second" {
  name = "TF-CI-Usage2"
}

resource "msteams_tenant_dial_plan" "test" {
  name        = "TF-CI-DialPlan"
  description = local.description
  normalization_rules = local.updated ? [
    {
      name        = "TF-CI-Extensions"
      pattern     = "^(\\d{4})$"
      translation = "+3314567$1"
      description = "Extensions"
    },
    {
      name                  = "TF-CI-Switchboard"
      pattern               = "^0$"
      translation           = "+33145670000"
      is_internal_extension = true
    },
    ] : [
    {
      name        = "TF-CI-Extensions"
      pattern     = "^(\\d{4})$"
      translation = "+3314567$1"
    },
  ]
}

resource "msteams_online_voice_route" "test" {
  name           = "TF-CI-Route"
  description    = local.description
  number_pattern = local.updated ? "^\\+33\\d{9}$" : "^\\+331\\d{8}$"
  # A position in the tenant's list of routes, after LocalRoute, then before it.
  priority = local.updated ? 0 : 1
  online_pstn_usages = local.updated ? [
    msteams_online_pstn_usage.test.name,
    msteams_online_pstn_usage.second.name,
  ] : [msteams_online_pstn_usage.test.name]
}

resource "msteams_online_voice_routing_policy" "test" {
  name        = "TF-CI-VoiceRouting"
  description = local.description
  online_pstn_usages = local.updated ? [
    msteams_online_pstn_usage.second.name,
    msteams_online_pstn_usage.test.name,
    ] : [
    msteams_online_pstn_usage.test.name,
    msteams_online_pstn_usage.second.name,
  ]
}

resource "msteams_emergency_calling_policy" "test" {
  name                          = "TF-CI-EmergencyCalling"
  description                   = local.description
  notification_mode             = var.test_notification_group != "" ? "NotificationOnly" : null
  notification_group            = var.test_notification_group != "" ? var.test_notification_group : null
  external_location_lookup_mode = local.updated ? "Disabled" : "Enabled"
}

resource "msteams_emergency_call_routing_policy" "test" {
  name                              = "TF-CI-EmergencyRouting"
  description                       = local.description
  allow_enhanced_emergency_services = local.updated
  emergency_numbers = local.updated ? [
    {
      dial_string = "112"
      dial_mask   = "15;17;18"
      pstn_usage  = msteams_online_pstn_usage.test.name
    },
    {
      dial_string = "911"
      pstn_usage  = msteams_online_pstn_usage.second.name
    },
    ] : [
    {
      dial_string = "112"
      pstn_usage  = msteams_online_pstn_usage.test.name
    },
  ]
}

# ---------------------------------------------------------------------------
# Group policy assignment, when a test group is given
# ---------------------------------------------------------------------------

resource "msteams_group_policy_assignment" "test" {
  count       = var.test_group_id != "" ? 1 : 0
  group_id    = var.test_group_id
  policy_type = "TeamsMeetingPolicy"
  policy_name = msteams_meeting_policy.test.name
  rank        = 1
}

# A voice policy type: group assignment covers them too.
resource "msteams_group_policy_assignment" "voice" {
  count       = var.test_group_id != "" ? 1 : 0
  group_id    = var.test_group_id
  policy_type = "OnlineVoiceRoutingPolicy"
  policy_name = msteams_online_voice_routing_policy.test.name
  rank        = 1
}

# ---------------------------------------------------------------------------
# Tenant-wide configurations: imported. Only the attributes driven by the
# variables above are ever written, and test/run.sh restores them.
# ---------------------------------------------------------------------------

import {
  to = msteams_meeting_configuration.this
  id = "Global"
}

resource "msteams_meeting_configuration" "this" {}

import {
  to = msteams_client_configuration.this
  id = "Global"
}

resource "msteams_client_configuration" "this" {}

import {
  to = msteams_guest_messaging_configuration.this
  id = "Global"
}

resource "msteams_guest_messaging_configuration" "this" {
  allow_memes = var.guest_allow_memes
}

import {
  to = msteams_guest_meeting_configuration.this
  id = "Global"
}

resource "msteams_guest_meeting_configuration" "this" {}

import {
  to = msteams_guest_calling_configuration.this
  id = "Global"
}

resource "msteams_guest_calling_configuration" "this" {}

import {
  to = msteams_tenant_federation_configuration.this
  id = "Global"
}

resource "msteams_tenant_federation_configuration" "this" {
  blocked_domains = var.federation_blocked_domains
}
