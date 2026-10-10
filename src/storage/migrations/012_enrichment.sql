CREATE TABLE enrichment_jobs (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,state TEXT NOT NULL CHECK(state IN ('queued','running','completed','failed','cancelled')),
 request BLOB NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL
);
CREATE INDEX enrichment_job_owner ON enrichment_jobs(owner,created_at,id);
CREATE TABLE enrichment_targets (
 job_id TEXT NOT NULL REFERENCES enrichment_jobs(id),source_id TEXT NOT NULL REFERENCES tracks(id),owner TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('queued','running','completed','failed','cancelled')),generation INTEGER NOT NULL CHECK(generation>0),
 target BLOB NOT NULL,issues BLOB NOT NULL,failure_code TEXT,preview_bytes INTEGER NOT NULL DEFAULT 0 CHECK(preview_bytes>=0),
 candidate_count INTEGER NOT NULL DEFAULT 0 CHECK(candidate_count>=0),PRIMARY KEY(job_id,source_id)
);
CREATE INDEX enrichment_target_queue ON enrichment_targets(state,job_id,source_id);
CREATE TABLE enrichment_candidates (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,job_id TEXT NOT NULL,source_id TEXT NOT NULL,generation INTEGER NOT NULL,
 object_id TEXT REFERENCES media_content_objects(id),expires_at TEXT NOT NULL,document BLOB NOT NULL,
 FOREIGN KEY(job_id,source_id) REFERENCES enrichment_targets(job_id,source_id)
);
CREATE INDEX enrichment_candidate_job ON enrichment_candidates(owner,job_id,source_id,id);
CREATE INDEX enrichment_candidate_object ON enrichment_candidates(object_id,expires_at);
