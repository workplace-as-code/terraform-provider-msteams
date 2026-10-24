# No surveys and no screenshots in feedback.
resource "msteams_feedback_policy" "quiet" {
  name                        = "No-Surveys"
  user_initiated_mode         = "Enabled"
  receive_surveys_mode        = "Disabled"
  allow_screenshot_collection = false
  allow_log_collection        = false
}
