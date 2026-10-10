CREATE TABLE crawl_coverage (
 run_id INTEGER PRIMARY KEY REFERENCES runs(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL CHECK (source_id = 'telegram:warpplus'),
 requested_url TEXT NOT NULL CHECK (requested_url = 'https://t.me/s/warpplus'),
 coverage_mode TEXT NOT NULL CHECK (coverage_mode = 'latest_preview'),
 page_limit INTEGER NOT NULL CHECK (page_limit = 1),
 fetched_at INTEGER,
 parsed_post_count INTEGER,
 min_message_id INTEGER,
 max_message_id INTEGER,
 CHECK (
  (fetched_at IS NULL AND parsed_post_count IS NULL AND min_message_id IS NULL AND max_message_id IS NULL)
  OR (fetched_at IS NOT NULL AND parsed_post_count IS NOT NULL AND (
   (parsed_post_count = 0 AND min_message_id IS NULL AND max_message_id IS NULL)
   OR (parsed_post_count > 0 AND min_message_id IS NOT NULL AND max_message_id IS NOT NULL
       AND min_message_id > 0 AND max_message_id >= min_message_id)
  ))
 )
);
