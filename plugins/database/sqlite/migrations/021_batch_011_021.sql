-- migration: 011_media_gc_backups.sql
CREATE TABLE media_garbage (
 id TEXT PRIMARY KEY,publication_id TEXT NOT NULL UNIQUE REFERENCES media_publications(id),
 object_id TEXT NOT NULL UNIQUE,state TEXT NOT NULL CHECK(state IN ('claimed','deleted')),
 created_at TEXT NOT NULL,updated_at TEXT NOT NULL
);
CREATE INDEX media_garbage_pending ON media_garbage(state,id);
CREATE TABLE media_backups (
 id TEXT PRIMARY KEY,database_path TEXT NOT NULL UNIQUE,
 state TEXT NOT NULL CHECK(state IN ('preparing','prepared','finished')),
 object_count INTEGER NOT NULL DEFAULT 0,missing_count INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL,updated_at TEXT NOT NULL
);
CREATE TABLE media_backup_objects (
 backup_id TEXT NOT NULL REFERENCES media_backups(id),object_id TEXT NOT NULL,
 owner TEXT NOT NULL,publication_id TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','copied','missing')),document BLOB NOT NULL,
 PRIMARY KEY(backup_id,object_id)
);
CREATE INDEX media_backup_pins ON media_backup_objects(object_id);

-- migration: 012_enrichment.sql
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

-- migration: 013_metadata_automation.sql
CREATE TABLE metadata_automation (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,scan_job_id TEXT NOT NULL REFERENCES jobs(id),scan_generation INTEGER NOT NULL,
 plan_digest TEXT NOT NULL,root_id TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),asset_id TEXT NOT NULL,asset_version TEXT NOT NULL,
 reason TEXT NOT NULL CHECK(reason IN ('automatic','importLyrics')),
 state TEXT NOT NULL CHECK(state IN ('queued','preparing','running','completed','skipped','failed')),
 policy BLOB,job_id TEXT REFERENCES enrichment_jobs(id),failure_code TEXT,created_at TEXT NOT NULL,
 UNIQUE(scan_job_id,scan_generation,source_id,reason)
);
CREATE INDEX metadata_automation_queue ON metadata_automation(state,created_at,id);
CREATE INDEX metadata_automation_scan ON metadata_automation(owner,scan_job_id,id);
CREATE TABLE metadata_automation_actions (
 trigger_id TEXT NOT NULL REFERENCES metadata_automation(id),field TEXT NOT NULL,language TEXT NOT NULL,
 expected_revision INTEGER NOT NULL,document BLOB NOT NULL,PRIMARY KEY(trigger_id,field,language)
);

-- migration: 014_writeback_jobs.sql
CREATE TABLE writeback_jobs (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),
 state TEXT NOT NULL CHECK(state IN ('queued','running','recovery','failed','succeeded','cancelled')),
 old_size INTEGER NOT NULL CHECK(old_size>0),work BLOB NOT NULL,error BLOB,result BLOB,commit_result BLOB,
 created_at TEXT NOT NULL,updated_at TEXT NOT NULL
);
CREATE INDEX writeback_job_queue ON writeback_jobs(state,updated_at,id);
CREATE INDEX writeback_source_fence ON writeback_jobs(owner,source_id,state);
CREATE TABLE writeback_operations (
 owner TEXT NOT NULL,key TEXT NOT NULL,request_digest TEXT NOT NULL,binding_digest TEXT NOT NULL,
 job_id TEXT NOT NULL REFERENCES writeback_jobs(id),expires_at TEXT NOT NULL,PRIMARY KEY(owner,key)
);

-- migration: 015_albums.sql
CREATE TABLE source_albums (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,provider_id TEXT NOT NULL,name TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision>=1),created_at TEXT NOT NULL,updated_at TEXT NOT NULL
);
CREATE INDEX source_albums_owner ON source_albums(owner,provider_id,id);
CREATE TABLE source_album_members (
 album_id TEXT NOT NULL REFERENCES source_albums(id),source_id TEXT NOT NULL REFERENCES tracks(id),
 position INTEGER NOT NULL CHECK(position>=0),PRIMARY KEY(album_id,source_id),UNIQUE(album_id,position)
);
CREATE TABLE releases (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,name TEXT NOT NULL,revision INTEGER NOT NULL CHECK(revision>=1),
 created_at TEXT NOT NULL,updated_at TEXT NOT NULL
);
CREATE INDEX releases_owner ON releases(owner,id);
CREATE TABLE release_albums (
 release_id TEXT NOT NULL REFERENCES releases(id),album_id TEXT NOT NULL REFERENCES source_albums(id),
 position INTEGER NOT NULL CHECK(position>=0),PRIMARY KEY(release_id,album_id),UNIQUE(release_id,position)
);
CREATE TABLE album_cover_bindings (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,target_kind TEXT NOT NULL CHECK(target_kind IN ('sourceAlbum','release')),
 target_id TEXT NOT NULL,object_id TEXT NOT NULL REFERENCES media_content_objects(id),
 target_revision INTEGER NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),asset_id TEXT NOT NULL REFERENCES source_assets(id),
 asset_version TEXT NOT NULL,created_at TEXT NOT NULL
);
CREATE INDEX album_cover_history ON album_cover_bindings(owner,target_kind,target_id,id);
CREATE TABLE album_cover_heads (
 owner TEXT NOT NULL,target_kind TEXT NOT NULL CHECK(target_kind IN ('sourceAlbum','release')),target_id TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision>=1),binding_id TEXT NOT NULL REFERENCES album_cover_bindings(id),
 manual_locked INTEGER NOT NULL CHECK(manual_locked IN (0,1)),PRIMARY KEY(owner,target_kind,target_id)
);
CREATE TABLE album_changes (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,owner TEXT NOT NULL,target_kind TEXT NOT NULL,target_id TEXT NOT NULL,
 document BLOB NOT NULL
);
CREATE INDEX album_changes_target ON album_changes(owner,target_kind,target_id,sequence);

-- migration: 016_derived_media.sql
CREATE TABLE media_derived_objects (
    id TEXT PRIMARY KEY,
    owner TEXT NOT NULL,
    publication_id TEXT NOT NULL UNIQUE REFERENCES media_publications(id),
    relationship_key TEXT NOT NULL,
    transform TEXT NOT NULL,
    parent_object_id TEXT,
    job_id TEXT,
    source_id TEXT,
    generation INTEGER NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('available','unavailable')),
    document BLOB NOT NULL,
    created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX media_derived_available ON media_derived_objects(owner,relationship_key) WHERE state='available';
CREATE INDEX media_derived_job ON media_derived_objects(owner,job_id,source_id,generation);

-- migration: 017_remote_playlists.sql
CREATE TABLE remote_playlists (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,instance_id TEXT NOT NULL,account_id TEXT NOT NULL,external_id TEXT NOT NULL,
 binding_revision INTEGER NOT NULL,revision INTEGER NOT NULL,document BLOB NOT NULL,
 UNIQUE(owner,instance_id,account_id,external_id)
);
CREATE INDEX remote_playlists_owner ON remote_playlists(owner,id);
CREATE TABLE remote_sources (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,instance_id TEXT NOT NULL,account_id TEXT NOT NULL,external_id TEXT NOT NULL,
 document BLOB NOT NULL,UNIQUE(owner,instance_id,account_id,external_id)
);
CREATE TABLE remote_playlist_snapshots (
 playlist_id TEXT NOT NULL REFERENCES remote_playlists(id),revision INTEGER NOT NULL,
 owner TEXT NOT NULL,document BLOB NOT NULL,PRIMARY KEY(playlist_id,revision)
);
CREATE TABLE remote_sync_jobs (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,playlist_id TEXT NOT NULL REFERENCES remote_playlists(id),
 state TEXT NOT NULL CHECK(state IN ('queued','running','succeeded','failed','cancelled')),
 generation INTEGER NOT NULL,binding_revision INTEGER NOT NULL,document BLOB NOT NULL,created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX remote_sync_active ON remote_sync_jobs(playlist_id) WHERE state IN ('queued','running');
CREATE INDEX remote_sync_queue ON remote_sync_jobs(state,created_at,id);

-- migration: 018_aggregate_playlists.sql
CREATE TABLE personal_playlists (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,rule_revision INTEGER NOT NULL,hide_revision INTEGER NOT NULL,
 snapshot_revision INTEGER NOT NULL,document BLOB NOT NULL
);
CREATE INDEX personal_playlists_owner ON personal_playlists(owner,id);
CREATE TABLE personal_playlist_snapshots (
 playlist_id TEXT NOT NULL REFERENCES personal_playlists(id),revision INTEGER NOT NULL,owner TEXT NOT NULL,
 document BLOB NOT NULL,PRIMARY KEY(playlist_id,revision)
);
CREATE TABLE playlist_hidden_rules (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,playlist_id TEXT NOT NULL REFERENCES personal_playlists(id),
 active INTEGER NOT NULL CHECK(active IN(0,1)),document BLOB NOT NULL
);
CREATE INDEX playlist_hidden_owner ON playlist_hidden_rules(owner,playlist_id,id);
CREATE TABLE playlist_changes (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,owner TEXT NOT NULL,playlist_id TEXT NOT NULL REFERENCES personal_playlists(id),
 document BLOB NOT NULL
);
CREATE INDEX playlist_changes_owner ON playlist_changes(owner,playlist_id,sequence);

-- migration: 019_timeline_evidence.sql
CREATE TABLE timeline_evidence (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, recording_id TEXT NOT NULL,
 from_source TEXT NOT NULL, to_source TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision>0), revoked_at TEXT,
 document BLOB NOT NULL, created_at TEXT NOT NULL
);
CREATE INDEX timeline_evidence_owner ON timeline_evidence(owner,from_source,to_source);

-- migration: 020_processed_cache.sql
CREATE TABLE processed_cache (
 id TEXT PRIMARY KEY, cache_key TEXT NOT NULL UNIQUE,
 owner TEXT NOT NULL, asset_id TEXT NOT NULL, asset_version TEXT NOT NULL,
 size_bytes INTEGER NOT NULL CHECK(size_bytes>0), sha256 TEXT NOT NULL,
 created_at TEXT NOT NULL, last_used_at TEXT NOT NULL, document BLOB NOT NULL
);
CREATE INDEX processed_cache_owner ON processed_cache(owner,last_used_at);

-- migration: 021_scan_retry.sql
ALTER TABLE jobs ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 0 CHECK(attempt_count>=0 AND attempt_count<=3);
ALTER TABLE jobs ADD COLUMN max_attempts INTEGER NOT NULL DEFAULT 3 CHECK(max_attempts=3);
ALTER TABLE jobs ADD COLUMN next_attempt_at TEXT;
ALTER TABLE jobs ADD COLUMN last_attempt_at TEXT;
DROP INDEX jobs_active_plan;
CREATE UNIQUE INDEX jobs_active_plan ON jobs(owner,target,plan_digest) WHERE state IN ('queued','running','retry_wait');
CREATE INDEX jobs_due ON jobs(state,next_attempt_at,created_at);
