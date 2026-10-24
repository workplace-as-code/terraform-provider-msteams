# Only internal attendees for webinars, no town halls.
resource "msteams_events_policy" "internal" {
  name              = "Internal-Events"
  description       = "Webinars for employees only"
  allow_webinars    = "Enabled"
  event_access_type = "EveryoneInCompanyExcludingGuests"
  allow_townhalls   = "Disabled"
}
