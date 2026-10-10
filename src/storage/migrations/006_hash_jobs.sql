CREATE TABLE hash_jobs (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, asset_id TEXT NOT NULL REFERENCES source_assets(id),
 generation INTEGER NOT NULL CHECK(generation>0), state TEXT NOT NULL,
 automatic INTEGER NOT NULL CHECK(automatic IN (0,1)), document BLOB NOT NULL,
 created_at TEXT NOT NULL, updated_at TEXT NOT NULL
);
CREATE UNIQUE INDEX hash_jobs_active ON hash_jobs(owner,asset_id) WHERE state IN ('queued','running');
CREATE INDEX hash_jobs_queue ON hash_jobs(state,automatic,created_at);
CREATE INDEX source_assets_hash ON source_assets(owner,file_sha512) WHERE current=1 AND file_sha512 IS NOT NULL;
CREATE TABLE hash_conflicts (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, asset_id TEXT NOT NULL REFERENCES source_assets(id),
 provider_evidence_id TEXT NOT NULL REFERENCES asset_evidence(id),
 verified_evidence_id TEXT NOT NULL REFERENCES asset_evidence(id),
 document BLOB NOT NULL, resolved_at TEXT,
 UNIQUE(provider_evidence_id,verified_evidence_id)
);
CREATE INDEX hash_conflicts_asset ON hash_conflicts(owner,asset_id);
