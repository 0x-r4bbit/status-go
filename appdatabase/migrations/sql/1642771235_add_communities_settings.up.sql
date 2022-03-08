CREATE TABLE communities_settings (
  community_id TEXT PRIMARY KEY ON CONFLICT REPLACE,
  message_archive_seeding_enabled BOOLEAN DEFAULT FALSE,
  message_archive_fetching_enabled BOOLEAN DEFAULT FALSE,
  last_message_archive_end_date INT
)
