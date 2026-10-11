CREATE TABLE crawl_sources (
 run_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL,
 channel TEXT NOT NULL CHECK(length(channel) BETWEEN 1 AND 64 AND channel NOT GLOB '*[^a-z0-9_]*'),
 position INTEGER NOT NULL CHECK(position BETWEEN 1 AND 32),
 page_limit INTEGER NOT NULL CHECK(page_limit BETWEEN 1 AND 100),
 status TEXT NOT NULL CHECK(status IN ('pending','running','completed','failed','cancelled','interrupted','skipped')),
 started_at INTEGER, finished_at INTEGER, recovered_at INTEGER, stop_reason TEXT,
 PRIMARY KEY(run_id,source_id), UNIQUE(run_id,position),
 CHECK(source_id = 'telegram:' || channel),
 CHECK(
  (status='pending' AND started_at IS NULL AND finished_at IS NULL AND recovered_at IS NULL AND stop_reason IS NULL)
  OR (status='running' AND started_at IS NOT NULL AND finished_at IS NULL AND recovered_at IS NULL AND stop_reason IS NULL)
  OR (status IN ('completed','failed','cancelled') AND started_at IS NOT NULL AND finished_at IS NOT NULL AND recovered_at IS NULL AND stop_reason IS NOT NULL)
  OR (status='interrupted' AND started_at IS NOT NULL AND finished_at IS NULL AND recovered_at IS NOT NULL AND stop_reason='interrupted')
  OR (status='skipped' AND started_at IS NULL AND stop_reason IS NOT NULL AND ((finished_at IS NOT NULL AND recovered_at IS NULL) OR (finished_at IS NULL AND recovered_at IS NOT NULL AND stop_reason='interrupted_before_start')))
 ),
 CHECK(status != 'completed' OR stop_reason IN ('preview_end','page_limit'))
);
CREATE TABLE crawl_pages (
 run_id INTEGER NOT NULL, source_id TEXT NOT NULL,
 page_number INTEGER NOT NULL CHECK(page_number BETWEEN 1 AND 100),
 requested_url TEXT NOT NULL, requested_at INTEGER NOT NULL,
 before_id INTEGER CHECK(before_id > 0),
 status TEXT NOT NULL CHECK(status IN ('requested','parsed','completed','failed','cancelled','interrupted')),
 fetched_at INTEGER, parsed_post_count INTEGER, min_message_id INTEGER, max_message_id INTEGER,
 next_before_id INTEGER CHECK(next_before_id > 0),
 finished_at INTEGER, recovered_at INTEGER, reason_code TEXT,
 PRIMARY KEY(run_id,source_id,page_number),
 FOREIGN KEY(run_id,source_id) REFERENCES crawl_sources(run_id,source_id) ON DELETE RESTRICT,
 CHECK((page_number=1 AND before_id IS NULL) OR (page_number>1 AND before_id IS NOT NULL)),
 CHECK(
  (fetched_at IS NULL AND parsed_post_count IS NULL AND min_message_id IS NULL AND max_message_id IS NULL AND next_before_id IS NULL)
  OR (fetched_at IS NOT NULL AND parsed_post_count IS NOT NULL AND (
   (parsed_post_count=0 AND min_message_id IS NULL AND max_message_id IS NULL AND next_before_id IS NULL)
   OR (parsed_post_count>0 AND min_message_id IS NOT NULL AND max_message_id IS NOT NULL AND min_message_id>0 AND max_message_id>=min_message_id)
  ))
 ),
 CHECK(next_before_id IS NULL OR (next_before_id<=min_message_id AND (before_id IS NULL OR next_before_id<before_id))),
 CHECK(
  (status='requested' AND fetched_at IS NULL AND finished_at IS NULL AND recovered_at IS NULL AND reason_code IS NULL)
  OR (status='parsed' AND fetched_at IS NOT NULL AND finished_at IS NULL AND recovered_at IS NULL AND reason_code IS NULL)
  OR (status='completed' AND fetched_at IS NOT NULL AND finished_at IS NOT NULL AND recovered_at IS NULL AND reason_code IS NULL)
  OR (status IN ('failed','cancelled') AND finished_at IS NOT NULL AND recovered_at IS NULL AND reason_code IS NOT NULL)
  OR (status='interrupted' AND finished_at IS NULL AND recovered_at IS NOT NULL AND reason_code='interrupted')
 )
);
CREATE TABLE crawl_page_observations (
 run_id INTEGER NOT NULL, sighting_id INTEGER NOT NULL, source_id TEXT NOT NULL, page_number INTEGER NOT NULL,
 PRIMARY KEY(run_id,sighting_id),
 FOREIGN KEY(run_id,sighting_id) REFERENCES crawl_observations(run_id,sighting_id) ON DELETE RESTRICT,
 FOREIGN KEY(run_id,source_id,page_number) REFERENCES crawl_pages(run_id,source_id,page_number) ON DELETE RESTRICT
);
CREATE INDEX crawl_page_links ON crawl_page_observations(run_id,source_id,page_number);
