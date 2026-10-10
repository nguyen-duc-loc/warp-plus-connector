CREATE TABLE candidates (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 key_text TEXT NOT NULL COLLATE BINARY UNIQUE CHECK (length(key_text) > 0)
);
CREATE TABLE runs (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 request_id TEXT NOT NULL UNIQUE CHECK (length(request_id) > 0),
 kind TEXT NOT NULL CHECK (kind IN ('crawl', 'validation')),
 status TEXT NOT NULL CHECK (status IN ('running','completed','failed','cancelled','interrupted')),
 started_at INTEGER NOT NULL,
 finished_at INTEGER,
 recovered_at INTEGER,
 reason_code TEXT,
 CHECK (
  (status = 'running' AND finished_at IS NULL AND recovered_at IS NULL AND reason_code IS NULL)
  OR (status = 'completed' AND finished_at IS NOT NULL AND recovered_at IS NULL AND reason_code IS NULL)
  OR (status IN ('failed','cancelled') AND finished_at IS NOT NULL AND recovered_at IS NULL AND reason_code IS NOT NULL)
  OR (status = 'interrupted' AND finished_at IS NULL AND recovered_at IS NOT NULL AND reason_code = 'interrupted')
 )
);
CREATE TABLE sightings (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 candidate_id INTEGER NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
 source_id TEXT NOT NULL CHECK (length(source_id) > 0),
 message_id TEXT NOT NULL CHECK (length(message_id) > 0),
 message_url TEXT,
 published_at INTEGER,
 UNIQUE (candidate_id, source_id, message_id)
);
CREATE TABLE crawl_observations (
 run_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE RESTRICT,
 sighting_id INTEGER NOT NULL REFERENCES sightings(id) ON DELETE RESTRICT,
 observed_at INTEGER NOT NULL,
 PRIMARY KEY (run_id, sighting_id)
);
CREATE TABLE validation_attempts (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 request_id TEXT NOT NULL UNIQUE CHECK (length(request_id) > 0),
 run_id INTEGER NOT NULL REFERENCES runs(id) ON DELETE RESTRICT,
 candidate_id INTEGER NOT NULL REFERENCES candidates(id) ON DELETE RESTRICT,
 backend_id TEXT NOT NULL CHECK (length(backend_id) > 0),
 backend_version TEXT NOT NULL CHECK (length(backend_version) > 0),
 started_at INTEGER NOT NULL,
 finished_at INTEGER,
 recovered_at INTEGER,
 outcome TEXT CHECK (outcome IN ('confirmed','invalid','inconclusive')),
 reason_code TEXT,
 evidence_json TEXT CHECK (evidence_json IS NULL OR json_valid(evidence_json)),
 CHECK (
  (outcome IS NULL AND finished_at IS NULL AND recovered_at IS NULL AND reason_code IS NULL AND evidence_json IS NULL)
  OR (outcome IS NOT NULL AND (
   (finished_at IS NOT NULL AND recovered_at IS NULL)
   OR (finished_at IS NULL AND recovered_at IS NOT NULL AND outcome = 'inconclusive' AND reason_code = 'interrupted')
  ) AND (
   (outcome = 'confirmed' AND reason_code IS NULL AND evidence_json IS NOT NULL)
   OR (outcome = 'invalid' AND reason_code IS NOT NULL AND evidence_json IS NOT NULL)
   OR (outcome = 'inconclusive' AND reason_code IS NOT NULL AND evidence_json IS NULL)
  ))
 )
);
CREATE INDEX sightings_candidate ON sightings(candidate_id, id);
CREATE INDEX observations_sighting ON crawl_observations(sighting_id, observed_at, run_id);
CREATE INDEX attempts_candidate ON validation_attempts(candidate_id, id);
CREATE INDEX attempts_run ON validation_attempts(run_id, id);
CREATE UNIQUE INDEX attempts_unresolved ON validation_attempts(candidate_id) WHERE outcome IS NULL;
CREATE INDEX runs_kind ON runs(kind, id);
