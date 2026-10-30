# Data sources built from resources. Each one fails the run if it reads
# another object than expected: a test policy read back by name once it
# exists, the org-wide Global policy, and the tenant's configurations.

data "msteams_messaging_policy" "test" {
  name       = msteams_messaging_policy.test.name
  depends_on = [msteams_messaging_policy.test]

  lifecycle {
    postcondition {
      condition     = self.id == msteams_messaging_policy.test.id
      error_message = "The data source read another policy than the test one."
    }
  }
}

data "msteams_meeting_policy" "test" {
  name       = msteams_meeting_policy.test.name
  depends_on = [msteams_meeting_policy.test]

  lifecycle {
    postcondition {
      condition     = self.id == msteams_meeting_policy.test.id
      error_message = "The data source read another policy than the test one."
    }
  }
}

data "msteams_calling_policy" "test" {
  name       = msteams_calling_policy.test.name
  depends_on = [msteams_calling_policy.test]

  lifecycle {
    postcondition {
      condition     = self.id == msteams_calling_policy.test.id
      error_message = "The data source read another policy than the test one."
    }
  }
}

data "msteams_app_setup_policy" "test" {
  name       = msteams_app_setup_policy.test.name
  depends_on = [msteams_app_setup_policy.test]

  lifecycle {
    postcondition {
      condition     = self.id == msteams_app_setup_policy.test.id
      error_message = "The data source read another policy than the test one."
    }
  }
}

data "msteams_channels_policy" "test" {
  name       = msteams_channels_policy.test.name
  depends_on = [msteams_channels_policy.test]

  lifecycle {
    postcondition {
      condition     = self.id == msteams_channels_policy.test.id
      error_message = "The data source read another policy than the test one."
    }
  }
}

data "msteams_update_management_policy" "test" {
  name       = msteams_update_management_policy.test.name
  depends_on = [msteams_update_management_policy.test]

  lifecycle {
    postcondition {
      condition     = self.id == msteams_update_management_policy.test.id
      error_message = "The data source read another policy than the test one."
    }
  }
}

data "msteams_feedback_policy" "test" {
  name       = msteams_feedback_policy.test.name
  depends_on = [msteams_feedback_policy.test]

  lifecycle {
    postcondition {
      condition     = self.id == msteams_feedback_policy.test.id
      error_message = "The data source read another policy than the test one."
    }
  }
}

data "msteams_events_policy" "test" {
  name       = msteams_events_policy.test.name
  depends_on = [msteams_events_policy.test]

  lifecycle {
    postcondition {
      condition     = self.id == msteams_events_policy.test.id
      error_message = "The data source read another policy than the test one."
    }
  }
}

data "msteams_meeting_policy" "global" {
  name = "Global"

  lifecycle {
    postcondition {
      condition     = self.id == "Global"
      error_message = "The data source did not read the Global policy."
    }
  }
}

data "msteams_meeting_configuration" "this" {
  lifecycle {
    postcondition {
      condition     = self.id == "Global"
      error_message = "The data source did not read the Global configuration."
    }
  }
}

data "msteams_client_configuration" "this" {
  lifecycle {
    postcondition {
      condition     = self.id == "Global"
      error_message = "The data source did not read the Global configuration."
    }
  }
}

data "msteams_guest_messaging_configuration" "this" {
  lifecycle {
    postcondition {
      condition     = self.id == "Global"
      error_message = "The data source did not read the Global configuration."
    }
  }
}

data "msteams_guest_meeting_configuration" "this" {
  lifecycle {
    postcondition {
      condition     = self.id == "Global"
      error_message = "The data source did not read the Global configuration."
    }
  }
}

data "msteams_guest_calling_configuration" "this" {
  lifecycle {
    postcondition {
      condition     = self.id == "Global"
      error_message = "The data source did not read the Global configuration."
    }
  }
}

data "msteams_tenant_federation_configuration" "this" {
  lifecycle {
    postcondition {
      condition     = self.id == "Global"
      error_message = "The data source did not read the Global configuration."
    }
  }
}
