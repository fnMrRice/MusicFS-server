-- migration: 001_foundation.sql
CREATE TABLE users (id TEXT PRIMARY KEY, role TEXT NOT NULL);
INSERT INTO users VALUES ('admin','admin');
CREATE TABLE auth_sessions (id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id), csrf TEXT NOT NULL, expires_at TEXT NOT NULL);
CREATE TABLE jobs (id TEXT PRIMARY KEY, owner TEXT NOT NULL, target TEXT NOT NULL, kind TEXT NOT NULL, generation INTEGER NOT NULL, state TEXT NOT NULL, observed INTEGER NOT NULL DEFAULT 0, complete INTEGER NOT NULL DEFAULT 0, error TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, last_success_at TEXT);
CREATE INDEX jobs_target ON jobs(owner,target,state);
CREATE TABLE idempotency (owner TEXT NOT NULL, route TEXT NOT NULL, key TEXT NOT NULL, digest TEXT NOT NULL, result BLOB NOT NULL, expires_at TEXT NOT NULL, PRIMARY KEY(owner,route,key));

-- migration: 002_library.sql
CREATE TABLE library_revision (singleton INTEGER PRIMARY KEY CHECK(singleton=1),revision INTEGER NOT NULL);
INSERT INTO library_revision VALUES(1,0);
CREATE TABLE tracks (id TEXT PRIMARY KEY,owner TEXT NOT NULL,provider TEXT NOT NULL,external_id TEXT NOT NULL,root_id TEXT NOT NULL,path TEXT NOT NULL,version TEXT NOT NULL,asset_id TEXT NOT NULL,mime TEXT NOT NULL,size INTEGER NOT NULL,document BLOB NOT NULL,UNIQUE(owner,provider,external_id));
CREATE INDEX tracks_provider ON tracks(owner,provider);
CREATE TABLE library_snapshots (id TEXT PRIMARY KEY,owner TEXT NOT NULL,provider TEXT NOT NULL,revision TEXT NOT NULL,expires_at TEXT NOT NULL);
CREATE TABLE library_snapshot_items (snapshot_id TEXT NOT NULL REFERENCES library_snapshots(id) ON DELETE CASCADE,position INTEGER NOT NULL,document BLOB NOT NULL,PRIMARY KEY(snapshot_id,position));
CREATE TABLE library_cursors (id TEXT PRIMARY KEY,snapshot_id TEXT NOT NULL REFERENCES library_snapshots(id) ON DELETE CASCADE,position INTEGER NOT NULL);

-- migration: 003_playback.sql
CREATE TABLE playback_sessions(id TEXT PRIMARY KEY,auth_session TEXT NOT NULL,user_id TEXT NOT NULL,description BLOB NOT NULL,state TEXT NOT NULL,expires_at TEXT NOT NULL);

-- migration: 004_configuration.sql
CREATE TABLE config_operations (
    id TEXT PRIMARY KEY,
    config_path TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    digest TEXT NOT NULL,
    previous_revision TEXT NOT NULL,
    next_revision TEXT NOT NULL,
    plugin_id TEXT NOT NULL,
    instance_id TEXT NOT NULL,
    enabled INTEGER NOT NULL CHECK(enabled IN (0, 1)),
    status TEXT NOT NULL CHECK(status IN ('prepared', 'saved', 'applied', 'notSaved', 'superseded')),
    UNIQUE(config_path, idempotency_key)
);

-- migration: 005_asset_evidence.sql
CREATE TABLE source_assets (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, track_id TEXT NOT NULL,
 version TEXT NOT NULL, current INTEGER NOT NULL CHECK(current IN (0,1)),
 document BLOB NOT NULL, created_at TEXT NOT NULL, invalidated_at TEXT,
 file_sha512 BLOB CHECK(file_sha512 IS NULL OR length(file_sha512)=64)
);
CREATE INDEX source_assets_track ON source_assets(owner,track_id,current);
CREATE UNIQUE INDEX source_assets_current ON source_assets(owner,track_id) WHERE current=1;
CREATE TABLE asset_evidence (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, asset_id TEXT NOT NULL REFERENCES source_assets(id),
 kind TEXT NOT NULL, origin TEXT NOT NULL, generation INTEGER NOT NULL CHECK(generation>0),
 status TEXT NOT NULL, digest BLOB CHECK(digest IS NULL OR length(digest)=64),
 document BLOB NOT NULL, created_at TEXT NOT NULL, invalidated_at TEXT,
 UNIQUE(asset_id,kind,origin,generation)
);
CREATE INDEX asset_evidence_lookup ON asset_evidence(owner,asset_id,kind,origin,generation);

-- migration: 006_hash_jobs.sql
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

-- migration: 007_scan_plans.sql
-- Discard unplanned scan task caches explicitly. No synthetic plan can prove
-- their authorized roots; library tracks, assets and evidence remain intact.
DELETE FROM idempotency WHERE substr(route,1,5)='scan/';
DELETE FROM jobs;
ALTER TABLE jobs ADD COLUMN plan BLOB NOT NULL;
ALTER TABLE jobs ADD COLUMN plan_digest TEXT NOT NULL;
CREATE UNIQUE INDEX jobs_active_plan ON jobs(owner,target,plan_digest) WHERE state IN ('queued','running');
CREATE TABLE provider_scan_leases (
 owner TEXT NOT NULL, provider TEXT NOT NULL, last_generation INTEGER NOT NULL CHECK(last_generation>=0),
 active_job_id TEXT REFERENCES jobs(id), PRIMARY KEY(owner,provider)
);

-- migration: 008_fingerprint_publications.sql
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

-- migration: 009_matching.sql
CREATE TABLE matching_revisions(owner TEXT PRIMARY KEY,revision INTEGER NOT NULL CHECK(revision>=0));
CREATE TABLE recordings (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL
);
CREATE INDEX recordings_owner ON recordings(owner,id);
CREATE TABLE recording_source_links (
 owner TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),recording_id TEXT REFERENCES recordings(id),
 decision TEXT NOT NULL,revision INTEGER NOT NULL,PRIMARY KEY(owner,source_id)
);
CREATE INDEX recording_links_members ON recording_source_links(owner,recording_id,source_id);
CREATE TABLE match_exclusions (
 owner TEXT NOT NULL,left_source TEXT NOT NULL REFERENCES tracks(id),right_source TEXT NOT NULL REFERENCES tracks(id),
 excluded INTEGER NOT NULL CHECK(excluded IN(0,1)),revision INTEGER NOT NULL,
 PRIMARY KEY(owner,left_source,right_source),CHECK(left_source<right_source)
);
CREATE TABLE link_changes (
 sequence INTEGER NOT NULL,owner TEXT NOT NULL,revision INTEGER NOT NULL,
 operation_id TEXT NOT NULL,document BLOB NOT NULL,PRIMARY KEY(owner,sequence)
);
CREATE INDEX link_changes_owner ON link_changes(owner,sequence);
CREATE TABLE matching_candidates (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,left_source TEXT NOT NULL,right_source TEXT NOT NULL,
 left_asset TEXT NOT NULL,right_asset TEXT NOT NULL,score INTEGER NOT NULL,status TEXT NOT NULL,
 binding_digest TEXT NOT NULL,document BLOB NOT NULL,created_at TEXT NOT NULL,CHECK(left_source<right_source)
);
CREATE INDEX matching_candidates_owner ON matching_candidates(owner,status,score DESC,id);

-- migration: 010_media_content.sql
CREATE TABLE media_content_objects (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),
 asset_id TEXT NOT NULL REFERENCES source_assets(id),publication_id TEXT NOT NULL UNIQUE REFERENCES media_publications(id),
 state TEXT NOT NULL CHECK(state IN ('available','unavailable')),document BLOB NOT NULL,created_at TEXT NOT NULL
);
CREATE INDEX media_content_owner ON media_content_objects(owner,source_id,id);
CREATE TABLE media_selection_revisions (
 owner TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),revision INTEGER NOT NULL CHECK(revision>=0),
 PRIMARY KEY(owner,source_id)
);
CREATE TABLE media_content_bindings (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),
 object_id TEXT NOT NULL REFERENCES media_content_objects(id),asset_id TEXT NOT NULL REFERENCES source_assets(id),
 field TEXT NOT NULL,language TEXT NOT NULL,current INTEGER NOT NULL CHECK(current IN (0,1)),
 document BLOB NOT NULL,created_at TEXT NOT NULL
);
CREATE INDEX media_binding_history ON media_content_bindings(owner,source_id,created_at,id);
CREATE TABLE media_content_slots (
 owner TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),field TEXT NOT NULL,language TEXT NOT NULL,
 binding_id TEXT NOT NULL REFERENCES media_content_bindings(id),manual_locked INTEGER NOT NULL CHECK(manual_locked IN (0,1)),
 PRIMARY KEY(owner,source_id,field,language)
);
CREATE TABLE media_selection_changes (
 owner TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),revision INTEGER NOT NULL,
 operation TEXT NOT NULL,field TEXT NOT NULL,language TEXT NOT NULL,
 previous_binding_id TEXT,next_binding_id TEXT,manual_locked INTEGER NOT NULL,
 created_at TEXT NOT NULL,PRIMARY KEY(owner,source_id,revision,field,language)
);
