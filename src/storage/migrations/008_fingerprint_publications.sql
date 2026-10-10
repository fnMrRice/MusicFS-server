CREATE TABLE fingerprint_jobs (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, asset_id TEXT NOT NULL REFERENCES source_assets(id),
 generation INTEGER NOT NULL CHECK(generation>0), state TEXT NOT NULL,
 automatic INTEGER NOT NULL CHECK(automatic IN (0,1)), request_digest TEXT NOT NULL,
 request_start_ms INTEGER NOT NULL, request_duration_ms INTEGER NOT NULL, config_revision TEXT NOT NULL,
 result_id TEXT, document BLOB NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX fingerprint_jobs_active ON fingerprint_jobs(owner,asset_id,request_digest) WHERE state IN ('queued','running');
CREATE INDEX fingerprint_jobs_queue ON fingerprint_jobs(state,automatic,created_at);
CREATE TABLE media_publications (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, object_id TEXT NOT NULL UNIQUE,
 purpose TEXT NOT NULL, target_id TEXT NOT NULL, generation INTEGER NOT NULL CHECK(generation>0),
 status TEXT NOT NULL, document BLOB NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE INDEX media_publications_pending ON media_publications(status,id);
CREATE UNIQUE INDEX media_publications_active_target ON media_publications(owner,purpose,target_id,generation) WHERE status IN ('prepared','written','published','referenced');
CREATE TABLE fingerprint_indexes (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, asset_id TEXT NOT NULL REFERENCES source_assets(id),
 job_id TEXT NOT NULL REFERENCES fingerprint_jobs(id), publication_id TEXT NOT NULL UNIQUE REFERENCES media_publications(id),
 object_id TEXT NOT NULL UNIQUE, request_digest TEXT NOT NULL, current INTEGER NOT NULL CHECK(current IN (0,1)),
 document BLOB NOT NULL, computed_at TEXT NOT NULL
);
CREATE UNIQUE INDEX fingerprint_current_request ON fingerprint_indexes(owner,asset_id,request_digest) WHERE current=1;
CREATE INDEX fingerprint_indexes_asset ON fingerprint_indexes(owner,asset_id,id);
