ALTER TABLE runs ADD COLUMN origin TEXT NOT NULL DEFAULT 'manual' CHECK(origin IN ('manual','scheduled'));
CREATE TABLE scheduler_state (
 id INTEGER PRIMARY KEY CHECK(id=1),
 last_observed_at INTEGER NOT NULL,
 crawl_coverage_json TEXT,
 crawl_due_at INTEGER,
 crawl_reason TEXT CHECK(crawl_reason IN ('initial','coverage_changed','completed','failed','cancelled','interrupted')),
 crawl_basis_at INTEGER,
 crawl_interval_ns INTEGER CHECK(crawl_interval_ns >= 0),
 crawl_basis_run_id INTEGER REFERENCES runs(id),
 validation_wait_reason TEXT,
 validation_wait_basis_at INTEGER,
 validation_retry_at INTEGER,
 validation_wait_interval_ns INTEGER CHECK(validation_wait_interval_ns > 0),
 validation_wait_observed_at INTEGER,
 validation_recovery_pending INTEGER NOT NULL DEFAULT 0 CHECK(validation_recovery_pending IN (0,1)),
 CHECK((crawl_coverage_json IS NULL AND crawl_due_at IS NULL AND crawl_reason IS NULL
 AND crawl_basis_at IS NULL AND crawl_interval_ns IS NULL AND crawl_basis_run_id IS NULL)
 OR (crawl_coverage_json IS NOT NULL AND crawl_due_at IS NOT NULL AND crawl_reason IS NOT NULL
 AND crawl_basis_at IS NOT NULL AND crawl_interval_ns IS NOT NULL)),
 CHECK((validation_wait_reason IS NULL AND validation_wait_basis_at IS NULL AND validation_retry_at IS NULL
 AND validation_wait_interval_ns IS NULL AND validation_wait_observed_at IS NULL)
 OR (validation_wait_reason IS NOT NULL AND validation_wait_basis_at IS NOT NULL AND validation_retry_at IS NOT NULL
 AND validation_wait_interval_ns IS NOT NULL AND validation_wait_observed_at IS NOT NULL))
);
CREATE TABLE candidate_schedule (
 candidate_id INTEGER PRIMARY KEY REFERENCES candidates(id),
 due_at INTEGER NOT NULL,
 reason TEXT NOT NULL CHECK(reason IN ('first_check','routine','failure')),
 basis_at INTEGER NOT NULL,
 interval_ns INTEGER NOT NULL CHECK(interval_ns >= 0),
 basis_run_id INTEGER REFERENCES runs(id),
 basis_attempt_id INTEGER REFERENCES validation_attempts(id),
 CHECK((reason='first_check' AND interval_ns=0 AND basis_run_id IS NULL AND basis_attempt_id IS NULL)
 OR (reason IN ('routine','failure') AND interval_ns>0 AND basis_run_id IS NOT NULL AND basis_attempt_id IS NOT NULL))
);
CREATE INDEX candidate_schedule_due ON candidate_schedule(due_at,candidate_id);

-- Completion policies belong to immutable replay payloads, independently of current eligibility.
ALTER TABLE runs ADD COLUMN schedule_completion_json TEXT;
ALTER TABLE validation_attempts ADD COLUMN schedule_completion_json TEXT;
ALTER TABLE validation_items ADD COLUMN schedule_completion_json TEXT;

CREATE INDEX validation_items_pending_candidate ON validation_items(candidate_id,run_id)
 WHERE status IN ('pending','active');
