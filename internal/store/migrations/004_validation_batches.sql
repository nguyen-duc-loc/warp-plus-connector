CREATE TABLE validation_batches (
 run_id INTEGER PRIMARY KEY REFERENCES runs(id),
 backend_id TEXT NOT NULL CHECK(length(backend_id)>0),
 backend_version TEXT NOT NULL CHECK(length(backend_version)>0),
 policy_json TEXT NOT NULL CHECK(json_valid(policy_json))
);
CREATE TABLE validation_items (
 run_id INTEGER NOT NULL REFERENCES validation_batches(run_id),
 candidate_id INTEGER NOT NULL REFERENCES candidates(id),
 ordinal INTEGER NOT NULL CHECK(ordinal>0),
 status TEXT NOT NULL CHECK(status IN ('pending','active','completed','skipped','interrupted')),
 stop_reason TEXT,
 started_at INTEGER,
 finished_at INTEGER,
 recovered_at INTEGER,
 PRIMARY KEY(run_id,candidate_id), UNIQUE(run_id,ordinal),
 CHECK((status='pending' AND started_at IS NULL AND finished_at IS NULL AND recovered_at IS NULL AND stop_reason IS NULL)
 OR (status='active' AND started_at IS NOT NULL AND finished_at IS NULL AND recovered_at IS NULL AND stop_reason IS NULL)
 OR (status='completed' AND started_at IS NOT NULL AND finished_at IS NOT NULL AND recovered_at IS NULL)
 OR (status='skipped' AND started_at IS NULL AND finished_at IS NOT NULL AND recovered_at IS NULL AND stop_reason IS NOT NULL)
 OR (status='interrupted' AND finished_at IS NULL AND recovered_at IS NOT NULL AND stop_reason='interrupted'))
);
CREATE TABLE validation_attempt_details (
 attempt_id INTEGER PRIMARY KEY REFERENCES validation_attempts(id),
 run_id INTEGER NOT NULL,
 candidate_id INTEGER NOT NULL,
 attempt_number INTEGER NOT NULL CHECK(attempt_number>0),
 failure_class TEXT CHECK(failure_class IN ('temporary','throttled','terminal','unsafe')),
 retry_wait_us INTEGER CHECK(retry_wait_us>0),
 retry_eligible_at INTEGER,
 completion_json TEXT CHECK(completion_json IS NULL OR json_valid(completion_json)),
 FOREIGN KEY(run_id,candidate_id) REFERENCES validation_items(run_id,candidate_id),
 UNIQUE(run_id,candidate_id,attempt_number),
 CHECK((retry_wait_us IS NULL)=(retry_eligible_at IS NULL))
);
CREATE TRIGGER batch_detail_identity BEFORE INSERT ON validation_attempt_details
WHEN NOT EXISTS(SELECT 1 FROM validation_attempts a WHERE a.id=NEW.attempt_id
 AND a.run_id=NEW.run_id AND a.candidate_id=NEW.candidate_id)
BEGIN SELECT RAISE(ABORT,'attempt identity mismatch'); END;
