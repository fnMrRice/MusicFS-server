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

-- migration: 022_catalog_queries.sql
CREATE TABLE catalog_queries (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, query BLOB NOT NULL, revision TEXT NOT NULL,
 total INTEGER NOT NULL CHECK(total>=0), expires_at TEXT NOT NULL,
 cancelled INTEGER NOT NULL DEFAULT 0 CHECK(cancelled IN (0,1)), idempotency_key TEXT NOT NULL
);
CREATE INDEX catalog_query_owner ON catalog_queries(owner,expires_at);
CREATE UNIQUE INDEX catalog_query_idempotency ON catalog_queries(owner,idempotency_key) WHERE idempotency_key<>'';
CREATE TABLE catalog_query_items (
 query_id TEXT NOT NULL REFERENCES catalog_queries(id) ON DELETE CASCADE,
 position INTEGER NOT NULL CHECK(position>=0), document BLOB NOT NULL, PRIMARY KEY(query_id,position)
);
CREATE TABLE catalog_query_cursors (
 id TEXT PRIMARY KEY, query_id TEXT NOT NULL REFERENCES catalog_queries(id) ON DELETE CASCADE,
 position INTEGER NOT NULL CHECK(position>=0), UNIQUE(query_id,position)
);

-- migration: 023_personal_library.sql
CREATE TABLE personal_profiles (
 owner TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,nickname TEXT
);
CREATE TABLE personal_favorites (
 owner TEXT NOT NULL,source_track_id TEXT NOT NULL,created_at TEXT NOT NULL,
 PRIMARY KEY(owner,source_track_id)
);
CREATE INDEX personal_favorites_order ON personal_favorites(owner,created_at DESC,source_track_id);
CREATE TABLE personal_recently_played (
 owner TEXT NOT NULL,source_track_id TEXT NOT NULL,played_at TEXT NOT NULL,
 PRIMARY KEY(owner,source_track_id)
);
CREATE INDEX personal_recently_played_order ON personal_recently_played(owner,played_at DESC,source_track_id);
CREATE TABLE personal_playback_receipts (
 owner TEXT NOT NULL,playback_session_id TEXT NOT NULL,source_track_id TEXT NOT NULL,
 recorded_at TEXT NOT NULL,expires_at TEXT NOT NULL,
 PRIMARY KEY(owner,playback_session_id)
);
CREATE TABLE personal_collection_revisions (
 owner TEXT NOT NULL,kind TEXT NOT NULL,revision INTEGER NOT NULL,
 PRIMARY KEY(owner,kind)
);
CREATE TABLE personal_collection_cursors (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,kind TEXT NOT NULL,revision TEXT NOT NULL,
 position INTEGER NOT NULL,expires_at TEXT NOT NULL
);
CREATE INDEX personal_collection_cursors_owner ON personal_collection_cursors(owner,kind);

-- migration: 024_plugin_management.sql
CREATE TABLE plugin_operations (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, document BLOB NOT NULL
);

-- migration: 025_matching_settings.sql
CREATE TABLE plugin_settings (
 owner TEXT NOT NULL, plugin_id TEXT NOT NULL, revision INTEGER NOT NULL,
 document BLOB NOT NULL, PRIMARY KEY(owner,plugin_id)
);
CREATE TABLE matching_runs (
 owner TEXT PRIMARY KEY, document BLOB NOT NULL
);
CREATE TABLE matching_action_receipts (
 owner TEXT NOT NULL, idempotency_key TEXT NOT NULL, document BLOB NOT NULL,
 PRIMARY KEY(owner,idempotency_key)
);

-- migration: 026_works.sql
CREATE TABLE works (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, content_type TEXT NOT NULL,
 title TEXT NOT NULL, description TEXT NOT NULL, revision INTEGER NOT NULL CHECK(revision>=1),
 document BLOB NOT NULL
);
CREATE INDEX works_owner ON works(owner,id);
CREATE INDEX works_type ON works(owner,content_type,id);
CREATE TABLE work_external_ids (
 work_id TEXT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
 namespace TEXT NOT NULL, value TEXT NOT NULL, PRIMARY KEY(work_id,namespace,value)
);
CREATE INDEX work_external_value ON work_external_ids(namespace,value,work_id);
CREATE TABLE work_credits (
 work_id TEXT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
 role TEXT NOT NULL, name TEXT NOT NULL, PRIMARY KEY(work_id,role,name)
);
CREATE INDEX work_credit_name ON work_credits(name,work_id);
CREATE TABLE work_tags (
 work_id TEXT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
 tag TEXT NOT NULL, PRIMARY KEY(work_id,tag)
);
CREATE INDEX work_tag_name ON work_tags(tag,work_id);

-- migration: 027_work_packages.sql
CREATE TABLE work_packages (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, work_id TEXT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
 provider_id TEXT NOT NULL, root_id TEXT NOT NULL, relative_path TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision>=1), document BLOB NOT NULL,
 UNIQUE(owner,provider_id,root_id,relative_path)
);
CREATE INDEX work_packages_work ON work_packages(owner,work_id,id);

-- migration: 028_work_package_kinds.sql
ALTER TABLE work_packages ADD COLUMN kind TEXT NOT NULL DEFAULT 'directory' CHECK(kind IN ('directory','cd_dump'));
CREATE INDEX work_packages_kind ON work_packages(owner,work_id,kind);

-- 旧包与导入回执都是直接保存的包文档，私有绑定同处根对象。
UPDATE work_packages SET document=json_set(document,'$.kind','directory');
UPDATE idempotency SET result=json_set(result,'$.kind','directory')
 WHERE json_valid(result)
 AND route='works/' || json_extract(result,'$.workId') || '/packages/import'
 AND json_type(result,'$.packageId')='text'
 AND json_type(result,'$.nodes')='array';

-- migration: 029_artwork_import.sql
-- 扩展扫描后导入原因，并完整保留已有触发器、策略、动作和外键。
CREATE TABLE metadata_automation_next (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,scan_job_id TEXT NOT NULL REFERENCES jobs(id),scan_generation INTEGER NOT NULL,
 plan_digest TEXT NOT NULL,root_id TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),asset_id TEXT NOT NULL,asset_version TEXT NOT NULL,
 reason TEXT NOT NULL CHECK(reason IN ('automatic','importLyrics','importArtwork')),
 state TEXT NOT NULL CHECK(state IN ('queued','preparing','running','completed','skipped','failed')),
 policy BLOB,job_id TEXT REFERENCES enrichment_jobs(id),failure_code TEXT,created_at TEXT NOT NULL,
 UNIQUE(scan_job_id,scan_generation,source_id,reason)
);
INSERT INTO metadata_automation_next SELECT * FROM metadata_automation;

CREATE TABLE metadata_automation_actions_next (
 trigger_id TEXT NOT NULL REFERENCES metadata_automation_next(id),field TEXT NOT NULL,language TEXT NOT NULL,
 expected_revision INTEGER NOT NULL,document BLOB NOT NULL,PRIMARY KEY(trigger_id,field,language)
);
INSERT INTO metadata_automation_actions_next SELECT * FROM metadata_automation_actions;
DROP TABLE metadata_automation_actions;
DROP TABLE metadata_automation;
ALTER TABLE metadata_automation_next RENAME TO metadata_automation;
ALTER TABLE metadata_automation_actions_next RENAME TO metadata_automation_actions;
CREATE INDEX metadata_automation_queue ON metadata_automation(state,created_at,id);
CREATE INDEX metadata_automation_scan ON metadata_automation(owner,scan_job_id,id);
-- migration: 030_recording_default_sources.sql
-- 录音默认来源按所有者隔离，未保存偏好时按来源质量自动选择。
CREATE TABLE recording_default_sources (
 owner TEXT NOT NULL,
 recording_id TEXT NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
 source_id TEXT NOT NULL,
 PRIMARY KEY(owner,recording_id),
 FOREIGN KEY(owner,source_id) REFERENCES recording_source_links(owner,source_id) ON DELETE CASCADE
);

-- 解除、拆分或合并将来源移出原录音时，旧录音恢复自动选择。
CREATE TRIGGER recording_default_sources_member_moved
AFTER UPDATE OF recording_id ON recording_source_links
WHEN OLD.recording_id IS NOT NEW.recording_id
BEGIN
 DELETE FROM recording_default_sources WHERE owner=OLD.owner AND recording_id=OLD.recording_id AND source_id=OLD.source_id;
END;
-- migration: 031_recording_covers.sql
-- 人工封面偏好独立修订；保留失效对象编号，不为媒体垃圾回收新增引用。
CREATE TABLE recording_cover_preferences (
 owner TEXT NOT NULL,
 recording_id TEXT NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
 revision INTEGER NOT NULL CHECK(revision>0),
 object_id TEXT,
 PRIMARY KEY(owner,recording_id)
);
-- migration: 032_metadata_refresh.sql
-- 扩展单次扫描的元数据刷新原因，保留已有触发记录、策略、动作及外键约束。
CREATE TABLE metadata_automation_next (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,scan_job_id TEXT NOT NULL REFERENCES jobs(id),scan_generation INTEGER NOT NULL,
 plan_digest TEXT NOT NULL,root_id TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),asset_id TEXT NOT NULL,asset_version TEXT NOT NULL,
 reason TEXT NOT NULL CHECK(reason IN ('automatic','importLyrics','importArtwork','refreshMetadata')),
 state TEXT NOT NULL CHECK(state IN ('queued','preparing','running','completed','skipped','failed')),
 policy BLOB,job_id TEXT REFERENCES enrichment_jobs(id),failure_code TEXT,created_at TEXT NOT NULL,
 UNIQUE(scan_job_id,scan_generation,source_id,reason)
);
INSERT INTO metadata_automation_next SELECT * FROM metadata_automation;

CREATE TABLE metadata_automation_actions_next (
 trigger_id TEXT NOT NULL REFERENCES metadata_automation_next(id),field TEXT NOT NULL,language TEXT NOT NULL,
 expected_revision INTEGER NOT NULL,document BLOB NOT NULL,PRIMARY KEY(trigger_id,field,language)
);
INSERT INTO metadata_automation_actions_next SELECT * FROM metadata_automation_actions;
DROP TABLE metadata_automation_actions;
DROP TABLE metadata_automation;
ALTER TABLE metadata_automation_next RENAME TO metadata_automation;
ALTER TABLE metadata_automation_actions_next RENAME TO metadata_automation_actions;
CREATE INDEX metadata_automation_queue ON metadata_automation(state,created_at,id);
CREATE INDEX metadata_automation_scan ON metadata_automation(owner,scan_job_id,id);

-- migration: 033_refresh_task_cancellation.sql
-- 取消意图先阻断发布；资源释放确认后记录完成时间，重启恢复可完成旧进程的取消。
CREATE TABLE refresh_task_cancellations (
 job_id TEXT PRIMARY KEY REFERENCES jobs(id),
 requested_at TEXT NOT NULL,
 completed_at TEXT
);
CREATE INDEX jobs_metadata_refresh ON jobs(owner,created_at DESC,id DESC)
 WHERE json_extract(plan,'$.refreshMetadata')=1;

-- migration: 034_fingerprint_analyzer_identity.sql
-- 分析器身份字段变更会改变证据摘要；旧明细不可冒充新身份，保留原文供审计。
CREATE TABLE retired_fingerprint_evidence (
 kind TEXT NOT NULL CHECK(kind IN ('job','index')),
 id TEXT NOT NULL,
 owner TEXT NOT NULL,
 document BLOB NOT NULL,
 retired_at TEXT NOT NULL,
 reason TEXT NOT NULL,
 PRIMARY KEY(kind,id)
);
INSERT INTO retired_fingerprint_evidence
 SELECT 'job',id,owner,document,strftime('%Y-%m-%dT%H:%M:%fZ','now'),'analyzer-identity-v2' FROM fingerprint_jobs;
INSERT INTO retired_fingerprint_evidence
 SELECT 'index',id,owner,document,strftime('%Y-%m-%dT%H:%M:%fZ','now'),'analyzer-identity-v2' FROM fingerprint_indexes;

-- 撤销派生证据和未完成发布，GC 继续遵守活动备份等引用保护。
UPDATE matching_candidates SET status='stale'
 WHERE status='pending' AND EXISTS(
 SELECT 1 FROM json_each(matching_candidates.document,'$.Candidate.reasons')
 WHERE json_extract(value,'$.kind')='fingerprintSimilar');
DELETE FROM fingerprint_indexes;
DELETE FROM fingerprint_jobs;
DELETE FROM idempotency WHERE substr(route,1,12)='fingerprint/';
UPDATE media_publications
 SET status='aborted',
     document=json_set(document,'$.Status','aborted','$.UpdatedAt',strftime('%Y-%m-%dT%H:%M:%fZ','now')),
     updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
 WHERE purpose='fingerprint';

-- migration: 035_multi_artist_tracks.sql
-- 仅规范化有效的歌手字符串数组；固定查询快照和曲目来源、关系保持原样。
WITH RECURSIVE
documents AS MATERIALIZED (
 SELECT id,document FROM tracks WHERE json_valid(document)
),
arrays AS MATERIALIZED (
 SELECT id,json_extract(document,'$.artists') AS artists FROM documents
 WHERE json_type(document,'$.artists')='array'
),
valid_arrays AS MATERIALIZED (
 SELECT id,artists FROM arrays
 WHERE NOT EXISTS(SELECT 1 FROM json_each(arrays.artists) WHERE type<>'text')
),
parts(id,artist_position,part_position,name,remainder) AS (
 -- 按字节定位 ASCII 分隔符，避免 SQLite 文本截取在内嵌 NUL 处截短名字。
 SELECT valid_arrays.id,artist.key,0,'',CAST(artist.value AS BLOB)
 FROM valid_arrays,json_each(valid_arrays.artists) AS artist
 UNION ALL
 SELECT id,artist_position,part_position+1,
        CAST(CASE WHEN instr(remainder,x'2f')=0 THEN remainder ELSE substr(remainder,1,instr(remainder,x'2f')-1) END AS TEXT),
        CASE WHEN instr(remainder,x'2f')=0 THEN x'' ELSE substr(remainder,instr(remainder,x'2f')+1) END
 FROM parts WHERE remainder<>x''
),
trimmed AS (
 -- 与 Go strings.TrimSpace 的 Unicode White_Space 字符集合一致。
 SELECT id,artist_position,part_position,
        trim(name,char(9,10,11,12,13,32,133,160,5760,8192,8193,8194,8195,8196,8197,8198,8199,8200,8201,8202,8232,8233,8239,8287,12288)) AS name
 FROM parts WHERE part_position>0
),
ranked AS (
 SELECT *,row_number() OVER(PARTITION BY id,name ORDER BY artist_position,part_position) AS occurrence
 FROM trimmed WHERE name<>''
),
normalized AS MATERIALIZED (
 SELECT valid_arrays.id,valid_arrays.artists AS previous,
        (SELECT json_group_array(name) FROM (
         SELECT name FROM ranked WHERE ranked.id=valid_arrays.id AND occurrence=1
         ORDER BY artist_position,part_position
        )) AS artists
 FROM valid_arrays
),
changed AS MATERIALIZED (
 SELECT id,artists FROM normalized
 WHERE json_array_length(previous)<>json_array_length(artists)
    OR EXISTS(SELECT 1 FROM json_each(previous) AS original
              WHERE original.value<>json_extract(artists,'$['||original.key||']'))
)
UPDATE tracks SET document=json_set(document,'$.artists',json(changed.artists))
 FROM changed WHERE tracks.id=changed.id;

-- 只有实际修改歌手时才递增曲库修订，让后续新查询观察到变化。
UPDATE library_revision SET revision=revision+1 WHERE changes()>0;
