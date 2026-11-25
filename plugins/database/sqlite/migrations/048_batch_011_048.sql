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

-- migration: 036_work_libraries.sql
-- 独立作品库默认不向普通曲目列表展示音轨；可选展示的专辑保存所有者选择。
ALTER TABLE works ADD COLUMN show_in_track_list INTEGER NOT NULL DEFAULT 0 CHECK(show_in_track_list IN (0,1));
UPDATE works SET document=json_set(document,'$.showInTrackList',json('false'));

-- 将既有音乐抓轨作品转为 CD 专辑，保留作品、音轨、副本与显式引用的身份。
UPDATE works SET content_type='cd_album', revision=revision+1,
 document=json_set(document,'$.contentType','cd_album','$.revision',revision+1,'$.updatedAt',strftime('%Y-%m-%dT%H:%M:%fZ','now'))
WHERE content_type='music' AND EXISTS (
 SELECT 1 FROM work_packages p WHERE p.owner=works.owner AND p.work_id=works.id AND p.kind='cd_dump'
);

UPDATE library_revision SET revision=revision+1 WHERE changes()>0;

-- 旧建档、编辑回执沿用各自记录的资料与修订，重放时仍能跳转到迁移后的作品库。
UPDATE idempotency AS i SET result=json_set(result,'$.contentType','cd_album','$.showInTrackList',json('false'))
WHERE i.route LIKE 'works/%' AND json_valid(i.result)
 AND json_extract(i.result,'$.contentType')='music' AND EXISTS (
 SELECT 1 FROM works w WHERE w.owner=i.owner AND w.content_type='cd_album'
 AND w.id=json_extract(i.result,'$.workId') AND i.route IN ('works/create','works/'||w.id||'/update')
);

-- migration: 037_music_catalog.sql
-- 来源观测保留原资产、播放和匹配身份。音乐资料拥有独立身份，可以完全没有来源。
CREATE TABLE music_tracks (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, title TEXT NOT NULL CHECK(length(trim(title))>0),
 cover_path TEXT, genre TEXT, year INTEGER, rating INTEGER CHECK(rating IS NULL OR (typeof(rating)='integer' AND rating BETWEEN 1 AND 5)),
 origin TEXT NOT NULL DEFAULT 'manual' CHECK(origin IN ('manual','source','recording')),
 metadata_source_id TEXT, metadata_locked INTEGER NOT NULL DEFAULT 0 CHECK(metadata_locked IN (0,1)),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>=1),
 created_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')),
 updated_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')),
 UNIQUE(owner,id)
);
CREATE INDEX music_tracks_title ON music_tracks(owner,title,id);
CREATE TABLE music_artists (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, name TEXT NOT NULL CHECK(length(trim(name))>0),
 UNIQUE(owner,name)
);
CREATE TABLE music_track_artists (
 track_id TEXT NOT NULL REFERENCES music_tracks(id) ON DELETE CASCADE,
 artist_id TEXT NOT NULL REFERENCES music_artists(id) ON DELETE CASCADE,
 position INTEGER NOT NULL CHECK(position>=0), PRIMARY KEY(track_id,artist_id), UNIQUE(track_id,position)
);
CREATE INDEX music_artist_tracks ON music_track_artists(artist_id,track_id);
CREATE TRIGGER music_credit_owner BEFORE INSERT ON music_track_artists
WHEN (SELECT owner FROM music_tracks WHERE id=NEW.track_id)<>(SELECT owner FROM music_artists WHERE id=NEW.artist_id)
BEGIN SELECT RAISE(ABORT,'Artist and track owners differ'); END;

CREATE TABLE music_albums (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, name TEXT NOT NULL CHECK(length(trim(name))>0),
 external_id TEXT, is_external INTEGER NOT NULL DEFAULT 0 CHECK(is_external IN (0,1)),
 cover_path TEXT, publisher TEXT, release_date TEXT,
 origin TEXT NOT NULL DEFAULT 'manual' CHECK(origin IN ('manual','source','cd','tag')),
 tag_provider TEXT, tag_name TEXT, metadata_locked INTEGER NOT NULL DEFAULT 0 CHECK(metadata_locked IN (0,1)),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>=1), UNIQUE(owner,tag_provider,tag_name)
);
CREATE INDEX music_album_names ON music_albums(owner,name,id);
CREATE TABLE music_manual_album_tracks (
 album_id TEXT NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
 track_id TEXT NOT NULL REFERENCES music_tracks(id) ON DELETE CASCADE,
 position INTEGER NOT NULL CHECK(position>=0), PRIMARY KEY(album_id,track_id)
);
CREATE INDEX music_track_albums ON music_manual_album_tracks(track_id,album_id);
CREATE TRIGGER music_album_member_owner BEFORE INSERT ON music_manual_album_tracks
WHEN (SELECT owner FROM music_tracks WHERE id=NEW.track_id)<>(SELECT owner FROM music_albums WHERE id=NEW.album_id)
BEGIN SELECT RAISE(ABORT,'Album and track owners differ'); END;
CREATE TRIGGER music_credit_update_owner BEFORE UPDATE OF track_id,artist_id ON music_track_artists
WHEN (SELECT owner FROM music_tracks WHERE id=NEW.track_id)<>(SELECT owner FROM music_artists WHERE id=NEW.artist_id)
BEGIN SELECT RAISE(ABORT,'Artist and track owners differ'); END;
CREATE TRIGGER music_album_update_owner BEFORE UPDATE OF track_id,album_id ON music_manual_album_tracks
WHEN (SELECT owner FROM music_tracks WHERE id=NEW.track_id)<>(SELECT owner FROM music_albums WHERE id=NEW.album_id)
BEGIN SELECT RAISE(ABORT,'Album and track owners differ'); END;

CREATE TABLE music_source_providers(provider_instance_id TEXT PRIMARY KEY,plugin_id TEXT NOT NULL CHECK(length(plugin_id)>0));
WITH observed AS (
 SELECT target,CASE WHEN json_valid(plan) THEN json_extract(plan,'$.pluginId') END AS id FROM jobs
), canonical AS (
 SELECT target,CASE id WHEN 'musicfs.localfiles' THEN 'musicfs.plugin.source.localfiles'
  WHEN 'musicfs.netease' THEN 'musicfs.plugin.source.netease'
  WHEN 'musicfs.qqmusic' THEN 'musicfs.plugin.source.qqmusic'
  WHEN 'musicfs.applemusic' THEN 'musicfs.plugin.source.applemusic' ELSE id END AS plugin_id FROM observed WHERE length(id)>0
)
INSERT INTO music_source_providers SELECT target,min(plugin_id) FROM canonical GROUP BY target HAVING count(DISTINCT plugin_id)=1;
CREATE TABLE music_sources (
 id TEXT PRIMARY KEY REFERENCES tracks(id) ON DELETE CASCADE, owner TEXT NOT NULL, track_id TEXT NOT NULL,
 plugin_id TEXT, provider_instance_id TEXT NOT NULL, external_id TEXT NOT NULL,
 created_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')),
 updated_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')),
 FOREIGN KEY(owner,track_id) REFERENCES music_tracks(owner,id), UNIQUE(owner,provider_instance_id,external_id)
);
CREATE INDEX music_track_sources ON music_sources(owner,track_id,id);
CREATE INDEX music_source_instance ON music_sources(provider_instance_id);
CREATE INDEX music_asset_observation_times ON source_assets(owner,track_id,created_at);
CREATE TRIGGER music_source_provider_insert AFTER INSERT ON music_source_providers BEGIN
 UPDATE music_sources SET plugin_id=NEW.plugin_id,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
 WHERE provider_instance_id=NEW.provider_instance_id AND plugin_id IS NOT NEW.plugin_id;
END;

-- 保存导入的专辑成员依据，显示时归并到逻辑曲目；跨盘重复文件和多来源录音均只产生一个成员。
CREATE TABLE music_album_source_refs (
 album_id TEXT NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
 source_id TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE, origin_id TEXT NOT NULL,
 position INTEGER NOT NULL CHECK(position>=0), PRIMARY KEY(album_id,origin_id,source_id)
);
CREATE INDEX music_source_album_refs ON music_album_source_refs(source_id,album_id);
CREATE INDEX music_package_album_refs ON music_album_source_refs(origin_id);
CREATE TRIGGER music_source_album_owner BEFORE INSERT ON music_album_source_refs
WHEN (SELECT owner FROM tracks WHERE id=NEW.source_id)<>(SELECT owner FROM music_albums WHERE id=NEW.album_id)
BEGIN SELECT RAISE(ABORT,'Album and source owners differ'); END;
CREATE TRIGGER music_source_album_update_owner BEFORE UPDATE OF source_id,album_id ON music_album_source_refs
WHEN (SELECT owner FROM tracks WHERE id=NEW.source_id)<>(SELECT owner FROM music_albums WHERE id=NEW.album_id)
BEGIN SELECT RAISE(ABORT,'Album and source owners differ'); END;
CREATE VIEW music_album_tracks AS
 SELECT album_id,track_id,min(position) AS position FROM (
  SELECT album_id,track_id,position FROM music_manual_album_tracks
  UNION ALL SELECT a.album_id,s.track_id,a.position FROM music_album_source_refs a
    JOIN music_sources s ON s.id=a.source_id JOIN music_albums b ON b.id=a.album_id AND b.owner=s.owner
 ) GROUP BY album_id,track_id;

-- 文本标签是插件观测。首次迁移和之后的扫描通过同一个导入入口写入规范资料。
CREATE VIEW music_observed_tracks AS
 SELECT t.id,t.owner,t.provider,t.external_id,coalesce(l.recording_id,t.id) AS track_id,
  coalesce(nullif(trim(json_extract(t.document,'$.title')),''),
           nullif(trim(json_extract(t.document,'$.displayName')),''),nullif(trim(t.path),''),nullif(trim(t.external_id),''),t.id) AS title,
  nullif(trim(json_extract(t.document,'$.genre')),'') AS genre, json_extract(t.document,'$.year') AS year,
  CASE WHEN json_type(t.document,'$.artists')='array' THEN json_extract(t.document,'$.artists') END AS artists,
  nullif(trim(json_extract(t.document,'$.album')),'') AS album
 FROM (SELECT id,owner,provider,external_id,path,CASE WHEN json_valid(document) THEN document ELSE '{}' END AS document FROM tracks) t
 LEFT JOIN recording_source_links l ON l.owner=t.owner AND l.source_id=t.id;

CREATE VIEW music_import_sources AS SELECT id FROM tracks;
CREATE TRIGGER music_import_source INSTEAD OF INSERT ON music_import_sources BEGIN
 INSERT INTO music_tracks(id,owner,title,genre,year,origin,metadata_source_id,created_at,updated_at)
 SELECT o.track_id,o.owner,o.title,o.genre,o.year,CASE WHEN o.track_id=o.id THEN 'source' ELSE 'recording' END,o.id,
  coalesce(r.created_at,(SELECT min(a.created_at) FROM source_assets a WHERE a.owner=o.owner AND a.track_id=o.id),
           strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  coalesce(r.updated_at,(SELECT max(a.created_at) FROM source_assets a WHERE a.owner=o.owner AND a.track_id=o.id),
           strftime('%Y-%m-%dT%H:%M:%fZ','now'))
 FROM music_observed_tracks o LEFT JOIN recordings r ON r.id=o.track_id AND r.owner=o.owner WHERE o.id=NEW.id
 ON CONFLICT(id) DO NOTHING;
 INSERT INTO music_sources(id,owner,track_id,plugin_id,provider_instance_id,external_id,created_at,updated_at)
 SELECT o.id,o.owner,o.track_id,p.plugin_id,o.provider,o.external_id,
  coalesce((SELECT min(a.created_at) FROM source_assets a WHERE a.owner=o.owner AND a.track_id=o.id),strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  coalesce((SELECT max(a.created_at) FROM source_assets a WHERE a.owner=o.owner AND a.track_id=o.id),strftime('%Y-%m-%dT%H:%M:%fZ','now'))
 FROM music_observed_tracks o LEFT JOIN music_source_providers p ON p.provider_instance_id=o.provider WHERE o.id=NEW.id
 ON CONFLICT(id) DO UPDATE SET track_id=excluded.track_id,plugin_id=excluded.plugin_id,
  provider_instance_id=excluded.provider_instance_id,external_id=excluded.external_id,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
 WHERE music_sources.track_id<>excluded.track_id OR music_sources.plugin_id IS NOT excluded.plugin_id
  OR music_sources.provider_instance_id<>excluded.provider_instance_id OR music_sources.external_id<>excluded.external_id;

 INSERT INTO music_albums(id,owner,name,origin,tag_provider,tag_name)
 SELECT lower(hex(randomblob(16))),owner,album,'tag',provider,album FROM music_observed_tracks WHERE id=NEW.id AND album IS NOT NULL
 ON CONFLICT(owner,tag_provider,tag_name) DO NOTHING;
 DELETE FROM music_album_source_refs WHERE source_id=NEW.id AND origin_id='tag';
 INSERT INTO music_album_source_refs(album_id,source_id,origin_id,position)
 SELECT a.id,o.id,'tag',0 FROM music_observed_tracks o JOIN music_albums a
 ON a.owner=o.owner AND a.tag_provider=o.provider AND a.tag_name=o.album WHERE o.id=NEW.id;
 INSERT INTO music_refresh_metadata SELECT track_id FROM music_observed_tracks WHERE id=NEW.id;
END;

-- 使用首次选取的元数据来源。变更播放首选来源不会改写曲名、署名和流派。
CREATE VIEW music_refresh_metadata AS SELECT id FROM music_tracks;
CREATE VIEW music_imported_credits AS
 SELECT m.id AS track_id,m.owner,trim(a.value) AS name,min(CAST(a.key AS INTEGER)) AS position
 FROM music_tracks m JOIN music_observed_tracks o ON o.id=m.metadata_source_id AND o.track_id=m.id,
 json_each(o.artists) a WHERE m.metadata_locked=0 AND a.type='text' AND length(trim(a.value))>0
 GROUP BY m.id,trim(a.value);
CREATE TRIGGER music_refresh_track INSTEAD OF INSERT ON music_refresh_metadata BEGIN
 UPDATE music_tracks SET metadata_source_id=(SELECT s.id FROM music_sources s WHERE s.owner=music_tracks.owner
  AND s.track_id=music_tracks.id ORDER BY s.id LIMIT 1)
 WHERE id=NEW.id AND origin<>'manual' AND NOT EXISTS(SELECT 1 FROM music_sources s WHERE s.id=metadata_source_id AND s.track_id=NEW.id);
 UPDATE music_tracks SET (title,genre,year)=(SELECT o.title,o.genre,o.year FROM music_observed_tracks o WHERE o.id=metadata_source_id),
  revision=revision+1
 WHERE id=NEW.id AND origin<>'manual' AND metadata_locked=0 AND EXISTS(SELECT 1 FROM music_observed_tracks o
  WHERE o.id=metadata_source_id AND (o.title IS NOT music_tracks.title OR o.genre IS NOT music_tracks.genre OR o.year IS NOT music_tracks.year));
 INSERT INTO music_artists(id,owner,name) SELECT lower(hex(randomblob(16))),owner,name FROM music_imported_credits
 WHERE track_id=NEW.id ON CONFLICT(owner,name) DO NOTHING;
 UPDATE music_tracks SET revision=revision+1 WHERE id=NEW.id AND origin<>'manual' AND metadata_locked=0 AND metadata_source_id IS NOT NULL
 AND (EXISTS(SELECT artist_id,position FROM music_track_artists WHERE track_id=NEW.id
    EXCEPT SELECT a.id,c.position FROM music_imported_credits c JOIN music_artists a ON a.owner=c.owner AND a.name=c.name WHERE c.track_id=NEW.id)
  OR EXISTS(SELECT a.id,c.position FROM music_imported_credits c JOIN music_artists a ON a.owner=c.owner AND a.name=c.name WHERE c.track_id=NEW.id
    EXCEPT SELECT artist_id,position FROM music_track_artists WHERE track_id=NEW.id));
 DELETE FROM music_track_artists WHERE track_id=NEW.id AND EXISTS(SELECT 1 FROM music_tracks WHERE id=NEW.id
  AND metadata_locked=0 AND origin<>'manual' AND metadata_source_id IS NOT NULL)
 AND NOT EXISTS(SELECT 1 FROM music_imported_credits c JOIN music_artists a ON a.owner=c.owner AND a.name=c.name
  WHERE c.track_id=NEW.id AND a.id=music_track_artists.artist_id AND c.position=music_track_artists.position);
 INSERT INTO music_track_artists(track_id,artist_id,position) SELECT c.track_id,a.id,c.position FROM music_imported_credits c
 JOIN music_artists a ON a.owner=c.owner AND a.name=c.name WHERE c.track_id=NEW.id ON CONFLICT(track_id,artist_id) DO NOTHING;
END;
INSERT INTO music_import_sources SELECT id FROM tracks ORDER BY id;
-- 旧插件实例统一编号时先迁移来源插件与标签专辑身份，保留已绑定的歌单和专辑编号。
CREATE TRIGGER music_provider_renamed BEFORE UPDATE OF provider ON tracks WHEN OLD.provider<>NEW.provider BEGIN
 INSERT INTO music_source_providers(provider_instance_id,plugin_id)
 SELECT NEW.provider,plugin_id FROM music_source_providers WHERE provider_instance_id=OLD.provider
 ON CONFLICT(provider_instance_id) DO NOTHING;
 UPDATE music_albums SET tag_provider=NEW.provider WHERE tag_provider=OLD.provider AND owner=NEW.owner AND origin='tag';
END;
CREATE TRIGGER music_provider_rename_finished AFTER UPDATE OF provider ON tracks WHEN OLD.provider<>NEW.provider BEGIN
 DELETE FROM music_source_providers WHERE provider_instance_id=OLD.provider AND NOT EXISTS(SELECT 1 FROM tracks WHERE provider=OLD.provider);
END;
CREATE TRIGGER music_new_observation AFTER INSERT ON tracks BEGIN INSERT INTO music_import_sources VALUES(NEW.id); END;
CREATE TRIGGER music_changed_observation AFTER UPDATE OF document,provider,external_id ON tracks
WHEN OLD.provider<>NEW.provider OR OLD.external_id<>NEW.external_id OR json_valid(OLD.document)<>json_valid(NEW.document)
 OR (json_valid(OLD.document) AND json_valid(NEW.document) AND (
  json_extract(OLD.document,'$.title','$.displayName','$.artists','$.album','$.genre','$.year')
  <>json_extract(NEW.document,'$.title','$.displayName','$.artists','$.album','$.genre','$.year')))
BEGIN INSERT INTO music_import_sources VALUES(NEW.id); END;
CREATE TRIGGER music_observation_timestamp AFTER UPDATE ON tracks
WHEN OLD.version<>NEW.version OR OLD.asset_id<>NEW.asset_id OR OLD.mime<>NEW.mime OR OLD.size<>NEW.size
 OR OLD.root_id<>NEW.root_id OR OLD.path<>NEW.path OR OLD.document IS NOT NEW.document
BEGIN UPDATE music_sources SET updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=NEW.id; END;
CREATE TRIGGER music_asset_timestamp AFTER UPDATE ON source_assets
WHEN OLD.document IS NOT NEW.document OR OLD.current<>NEW.current OR OLD.invalidated_at IS NOT NEW.invalidated_at OR OLD.file_sha512 IS NOT NEW.file_sha512
BEGIN UPDATE music_sources SET updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=NEW.track_id AND owner=NEW.owner; END;
CREATE TRIGGER music_new_recording_link AFTER INSERT ON recording_source_links BEGIN
 INSERT INTO music_import_sources VALUES(NEW.source_id);
END;
CREATE TRIGGER music_changed_recording_link AFTER UPDATE OF recording_id ON recording_source_links
WHEN NEW.recording_id IS NOT OLD.recording_id BEGIN
 INSERT INTO music_import_sources VALUES(NEW.source_id);
 INSERT INTO music_refresh_metadata VALUES(coalesce(OLD.recording_id,OLD.source_id));
END;
CREATE TRIGGER music_removed_recording_link AFTER DELETE ON recording_source_links BEGIN
 INSERT INTO music_import_sources VALUES(OLD.source_id);
 INSERT INTO music_refresh_metadata VALUES(OLD.recording_id);
END;

-- 单曲首次归并时延续人工资料及手工专辑成员。已有独立建档或人工编辑的目标拥有自己的资料。
CREATE TRIGGER music_merge_curated_track AFTER UPDATE OF track_id ON music_sources
WHEN OLD.track_id=OLD.id AND NEW.track_id<>OLD.track_id BEGIN
 INSERT INTO music_manual_album_tracks(album_id,track_id,position)
 SELECT album_id,NEW.track_id,position FROM music_manual_album_tracks WHERE track_id=OLD.track_id
 ON CONFLICT(album_id,track_id) DO NOTHING;
 DELETE FROM music_track_artists WHERE track_id=NEW.track_id
 AND EXISTS(SELECT 1 FROM music_tracks WHERE id=NEW.track_id AND origin='recording' AND metadata_locked=0)
 AND EXISTS(SELECT 1 FROM music_tracks WHERE id=OLD.track_id AND metadata_locked=1);
 INSERT INTO music_track_artists(track_id,artist_id,position)
 SELECT NEW.track_id,a.artist_id,a.position FROM music_track_artists a JOIN music_tracks m ON m.id=a.track_id AND m.metadata_locked=1
 WHERE a.track_id=OLD.track_id AND EXISTS(SELECT 1 FROM music_tracks WHERE id=NEW.track_id AND origin='recording' AND metadata_locked=0);
 UPDATE music_tracks SET (title,cover_path,genre,year,rating,metadata_locked)=(SELECT title,cover_path,genre,year,rating,metadata_locked
  FROM music_tracks WHERE id=OLD.track_id), revision=revision+1
 WHERE id=NEW.track_id AND origin='recording' AND metadata_locked=0 AND EXISTS(SELECT 1 FROM music_tracks WHERE id=OLD.track_id AND metadata_locked=1);
END;

-- 来源专辑、CD 插件专辑和手工专辑各自保留稳定身份。
INSERT INTO music_albums(id,owner,name,origin) SELECT id,owner,name,'source' FROM source_albums;
CREATE TRIGGER music_new_source_album AFTER INSERT ON source_albums BEGIN
 INSERT INTO music_albums(id,owner,name,origin) VALUES(NEW.id,NEW.owner,NEW.name,'source');
END;
CREATE TRIGGER music_changed_source_album AFTER UPDATE OF name ON source_albums BEGIN
 UPDATE music_albums SET name=NEW.name,revision=revision+1 WHERE id=NEW.id AND metadata_locked=0 AND name<>NEW.name;
END;
CREATE TRIGGER music_removed_source_album AFTER DELETE ON source_albums BEGIN
 DELETE FROM music_albums WHERE id=OLD.id AND origin='source';
END;
INSERT INTO music_album_source_refs SELECT album_id,source_id,'source',position FROM source_album_members;
CREATE TRIGGER music_new_source_member AFTER INSERT ON source_album_members BEGIN
 INSERT INTO music_album_source_refs VALUES(NEW.album_id,NEW.source_id,'source',NEW.position);
END;
CREATE TRIGGER music_changed_source_member AFTER UPDATE ON source_album_members BEGIN
 DELETE FROM music_album_source_refs WHERE origin_id='source' AND album_id=OLD.album_id AND source_id=OLD.source_id;
 INSERT INTO music_album_source_refs VALUES(NEW.album_id,NEW.source_id,'source',NEW.position);
END;
CREATE TRIGGER music_removed_source_member AFTER DELETE ON source_album_members BEGIN
 DELETE FROM music_album_source_refs WHERE origin_id='source' AND album_id=OLD.album_id AND source_id=OLD.source_id;
END;

CREATE VIEW music_import_cd_albums AS SELECT id FROM works WHERE content_type='cd_album';
CREATE TRIGGER music_import_cd INSTEAD OF INSERT ON music_import_cd_albums BEGIN
 INSERT INTO music_albums(id,owner,name,external_id,is_external,origin)
 SELECT w.id,w.owner,w.title,(SELECT json_extract(x.value,'$.value') FROM json_each(w.document,'$.externalIds') x
  WHERE json_extract(x.value,'$.namespace')='catalog_number' ORDER BY x.key LIMIT 1),1,'cd'
 FROM works w WHERE w.id=NEW.id AND w.content_type='cd_album'
 ON CONFLICT(id) DO UPDATE SET name=excluded.name,external_id=excluded.external_id,revision=music_albums.revision+1
 WHERE music_albums.metadata_locked=0 AND (music_albums.name<>excluded.name OR music_albums.external_id IS NOT excluded.external_id);
END;
INSERT INTO music_import_cd_albums SELECT id FROM works WHERE content_type='cd_album';
CREATE TRIGGER music_new_cd_album AFTER INSERT ON works WHEN NEW.content_type='cd_album' BEGIN
 INSERT INTO music_import_cd_albums VALUES(NEW.id);
END;
CREATE TRIGGER music_changed_cd_album AFTER UPDATE OF document ON works WHEN NEW.content_type='cd_album' BEGIN
 INSERT INTO music_import_cd_albums VALUES(NEW.id);
END;
CREATE TRIGGER music_removed_cd_album AFTER DELETE ON works WHEN OLD.content_type='cd_album' BEGIN
 DELETE FROM music_albums WHERE id=OLD.id AND origin='cd';
END;
CREATE VIEW music_import_cd_packages AS SELECT id FROM work_packages;
CREATE TRIGGER music_import_cd_package INSTEAD OF INSERT ON music_import_cd_packages BEGIN
 DELETE FROM music_album_source_refs WHERE origin_id=NEW.id;
 INSERT INTO music_album_source_refs(album_id,source_id,origin_id,position)
 SELECT a.id,s.id,p.id,min(CAST(n.key AS INTEGER)) FROM work_packages p JOIN music_albums a
 ON a.id=p.work_id AND a.owner=p.owner AND a.origin='cd', json_each(p.document,'$.nodes') n
 JOIN music_sources s ON s.id=json_extract(n.value,'$.sourceTrackId') AND s.owner=p.owner
 WHERE p.id=NEW.id AND json_extract(n.value,'$.kind')='audio' GROUP BY a.id,s.id,p.id;
END;
INSERT INTO music_import_cd_packages SELECT id FROM work_packages;
CREATE TRIGGER music_new_cd_package AFTER INSERT ON work_packages BEGIN INSERT INTO music_import_cd_packages VALUES(NEW.id); END;
CREATE TRIGGER music_changed_cd_package AFTER UPDATE OF document ON work_packages BEGIN INSERT INTO music_import_cd_packages VALUES(NEW.id); END;
CREATE TRIGGER music_removed_cd_package AFTER DELETE ON work_packages BEGIN DELETE FROM music_album_source_refs WHERE origin_id=OLD.id; END;

-- 封面保存可鉴权读取的媒体文件地址。自动选择遵循已绑定的录音偏好、面积、短边和稳定编号。
CREATE VIEW music_track_covers AS
 SELECT s.track_id,b.object_id,p.object_id AS preferred,
  CAST(coalesce(json_extract(o.document,'$.width'),0) AS INTEGER) AS width,
  CAST(coalesce(json_extract(o.document,'$.height'),0) AS INTEGER) AS height
 FROM music_sources s JOIN tracks t ON t.id=s.id AND t.owner=s.owner
 JOIN source_assets a ON a.owner=t.owner AND a.id=t.asset_id AND a.track_id=t.id AND a.version=t.version AND a.current=1
 JOIN media_content_slots h ON h.owner=t.owner AND h.source_id=t.id AND h.field='artwork' AND h.language=''
 JOIN media_content_bindings b ON b.id=h.binding_id AND b.owner=h.owner AND b.source_id=h.source_id AND b.asset_id=t.asset_id
  AND b.field=h.field AND b.language=h.language AND b.current=1 AND json_extract(b.document,'$.target.assetVersion')=t.version
 JOIN media_content_objects o ON o.id=b.object_id AND o.owner=b.owner AND o.source_id=t.id AND o.state='available'
 LEFT JOIN recording_cover_preferences p ON p.owner=s.owner AND p.recording_id=s.track_id
 WHERE CASE WHEN json_valid(t.document) THEN json_extract(t.document,'$.availability') END='available';
CREATE VIEW music_refresh_cover AS SELECT id FROM music_tracks;
CREATE TRIGGER music_set_cover INSTEAD OF INSERT ON music_refresh_cover BEGIN
 UPDATE music_tracks SET cover_path=u.path,revision=revision+1
 FROM (SELECT (SELECT '/api/v1/media/'||object_id FROM music_track_covers WHERE track_id=NEW.id
  ORDER BY (object_id IS preferred) DESC,width*height DESC,min(width,height) DESC,object_id LIMIT 1) AS path) u
 WHERE music_tracks.id=NEW.id AND music_tracks.metadata_locked=0 AND music_tracks.cover_path IS NOT u.path;
END;
INSERT INTO music_refresh_cover SELECT DISTINCT track_id FROM music_track_covers;
-- 初始回填沿用历史时间。正常运行后，从曲目实际修订统一推进更新时间。
CREATE TRIGGER music_track_updated_at AFTER UPDATE OF revision ON music_tracks
WHEN NEW.revision<>OLD.revision AND NEW.updated_at=OLD.updated_at BEGIN
 UPDATE music_tracks SET updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=NEW.id;
END;
CREATE TRIGGER music_linked_cover AFTER UPDATE OF track_id ON music_sources WHEN NEW.track_id<>OLD.track_id BEGIN
 INSERT INTO music_refresh_cover VALUES(OLD.track_id),(NEW.track_id);
END;
CREATE TRIGGER music_track_version_cover AFTER UPDATE OF document,asset_id,version ON tracks
WHEN NEW.asset_id<>OLD.asset_id OR NEW.version<>OLD.version
 OR (CASE WHEN json_valid(NEW.document) THEN json_extract(NEW.document,'$.availability') END)
 IS NOT (CASE WHEN json_valid(OLD.document) THEN json_extract(OLD.document,'$.availability') END) BEGIN
 INSERT INTO music_refresh_cover SELECT track_id FROM music_sources WHERE id=NEW.id;
END;
CREATE TRIGGER music_new_artwork AFTER INSERT ON media_content_slots WHEN NEW.field='artwork' BEGIN
 INSERT INTO music_refresh_cover SELECT track_id FROM music_sources WHERE id=NEW.source_id;
END;
CREATE TRIGGER music_changed_artwork AFTER UPDATE ON media_content_slots WHEN NEW.field='artwork' BEGIN
 INSERT INTO music_refresh_cover SELECT track_id FROM music_sources WHERE id=NEW.source_id;
END;
CREATE TRIGGER music_removed_artwork AFTER DELETE ON media_content_slots WHEN OLD.field='artwork' BEGIN
 INSERT INTO music_refresh_cover SELECT track_id FROM music_sources WHERE id=OLD.source_id;
END;
CREATE TRIGGER music_selected_artwork AFTER INSERT ON recording_cover_preferences BEGIN INSERT INTO music_refresh_cover VALUES(NEW.recording_id); END;
CREATE TRIGGER music_reselected_artwork AFTER UPDATE ON recording_cover_preferences BEGIN INSERT INTO music_refresh_cover VALUES(NEW.recording_id); END;
CREATE TRIGGER music_deselected_artwork AFTER DELETE ON recording_cover_preferences BEGIN INSERT INTO music_refresh_cover VALUES(OLD.recording_id); END;
CREATE TRIGGER music_artwork_state AFTER UPDATE OF state ON media_content_objects WHEN OLD.state<>NEW.state BEGIN
 INSERT INTO music_refresh_cover SELECT track_id FROM music_sources WHERE id=NEW.source_id;
END;
CREATE VIEW music_refresh_album_cover AS SELECT id FROM music_albums;
CREATE TRIGGER music_set_album_cover INSTEAD OF INSERT ON music_refresh_album_cover BEGIN
 UPDATE music_albums SET cover_path=u.path,revision=revision+1 FROM (
 SELECT (SELECT '/api/v1/media/'||b.object_id FROM album_cover_heads h
 JOIN album_cover_bindings b ON b.id=h.binding_id AND b.owner=h.owner
 JOIN source_albums a ON a.id=h.target_id AND a.owner=h.owner AND a.revision=b.target_revision
 JOIN tracks t ON t.id=b.source_id AND t.owner=b.owner AND t.asset_id=b.asset_id AND t.version=b.asset_version
 JOIN media_content_objects o ON o.id=b.object_id AND o.state='available'
 WHERE h.target_kind='sourceAlbum' AND h.target_id=NEW.id) AS path) u
 WHERE music_albums.id=NEW.id AND music_albums.origin='source' AND music_albums.metadata_locked=0 AND music_albums.cover_path IS NOT u.path;
END;
INSERT INTO music_refresh_album_cover SELECT target_id FROM album_cover_heads WHERE target_kind='sourceAlbum';
CREATE TRIGGER music_new_album_cover AFTER INSERT ON album_cover_heads WHEN NEW.target_kind='sourceAlbum'
BEGIN INSERT INTO music_refresh_album_cover VALUES(NEW.target_id); END;
CREATE TRIGGER music_updated_album_cover AFTER UPDATE ON album_cover_heads WHEN NEW.target_kind='sourceAlbum'
BEGIN INSERT INTO music_refresh_album_cover VALUES(NEW.target_id); END;
CREATE TRIGGER music_album_cover_revision AFTER UPDATE OF revision ON source_albums
BEGIN INSERT INTO music_refresh_album_cover VALUES(NEW.id); END;
CREATE TRIGGER music_album_cover_removed AFTER DELETE ON album_cover_heads WHEN OLD.target_kind='sourceAlbum'
BEGIN INSERT INTO music_refresh_album_cover VALUES(OLD.target_id); END;
CREATE INDEX music_album_artwork_objects ON album_cover_bindings(object_id,source_id);
CREATE INDEX music_album_artwork_sources ON album_cover_bindings(source_id,asset_id);
CREATE TRIGGER music_album_artwork_state AFTER UPDATE OF state ON media_content_objects WHEN OLD.state<>NEW.state BEGIN
 INSERT INTO music_refresh_album_cover SELECT DISTINCT target_id FROM album_cover_bindings WHERE object_id=NEW.id AND target_kind='sourceAlbum';
END;
CREATE TRIGGER music_album_artwork_version AFTER UPDATE OF asset_id,version ON tracks WHEN NEW.asset_id<>OLD.asset_id OR NEW.version<>OLD.version BEGIN
 INSERT INTO music_refresh_album_cover SELECT DISTINCT target_id FROM album_cover_bindings WHERE source_id=NEW.id AND target_kind='sourceAlbum';
END;

-- 旧记录可能只有日期或带偏移；统一到 UTC，保留原本已带 Z 的亚秒精度。
UPDATE music_tracks SET created_at=coalesce(strftime('%Y-%m-%dT%H:%M:%fZ',created_at),strftime('%Y-%m-%dT%H:%M:%fZ','now')) WHERE created_at NOT GLOB '????-??-??T??:??:??*Z';
UPDATE music_tracks SET updated_at=coalesce(strftime('%Y-%m-%dT%H:%M:%fZ',updated_at),strftime('%Y-%m-%dT%H:%M:%fZ','now')) WHERE updated_at NOT GLOB '????-??-??T??:??:??*Z';
UPDATE music_sources SET created_at=coalesce(strftime('%Y-%m-%dT%H:%M:%fZ',created_at),strftime('%Y-%m-%dT%H:%M:%fZ','now')) WHERE created_at NOT GLOB '????-??-??T??:??:??*Z';
UPDATE music_sources SET updated_at=coalesce(strftime('%Y-%m-%dT%H:%M:%fZ',updated_at),strftime('%Y-%m-%dT%H:%M:%fZ','now')) WHERE updated_at NOT GLOB '????-??-??T??:??:??*Z';

-- migration: 038_music_playlist_entries.sql
-- 普通歌单保留每次出现的稳定身份，特殊歌单使用空 binding_id。
CREATE TABLE music_playlist_entries (
 owner TEXT NOT NULL, playlist_id TEXT NOT NULL, binding_id TEXT NOT NULL, entry_id TEXT NOT NULL,
 track_id TEXT NOT NULL, forced_source_id TEXT, position INTEGER NOT NULL CHECK(position>=0),
 source_contribution INTEGER NOT NULL DEFAULT 0 CHECK(source_contribution IN (0,1) AND (source_contribution=0 OR forced_source_id IS NOT NULL)),
 created_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')),
 updated_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')),
 play_count INTEGER NOT NULL DEFAULT 0 CHECK(play_count>=0),
 PRIMARY KEY(owner,playlist_id,binding_id,entry_id),
 FOREIGN KEY(owner,track_id) REFERENCES music_tracks(owner,id)
);
CREATE INDEX music_playlist_track ON music_playlist_entries(owner,track_id,playlist_id);
CREATE INDEX music_playlist_positions ON music_playlist_entries(owner,playlist_id,binding_id,position,entry_id);
CREATE TRIGGER music_playlist_source_owner BEFORE INSERT ON music_playlist_entries
WHEN NEW.forced_source_id IS NOT NULL AND NOT EXISTS(
 SELECT 1 FROM music_sources WHERE id=NEW.forced_source_id AND owner=NEW.owner)
BEGIN SELECT RAISE(ABORT,'Playlist source does not belong to owner'); END;
CREATE TRIGGER music_playlist_updated_source_owner BEFORE UPDATE OF owner,forced_source_id ON music_playlist_entries
WHEN NEW.forced_source_id IS NOT NULL AND NOT EXISTS(
 SELECT 1 FROM music_sources WHERE id=NEW.forced_source_id AND owner=NEW.owner)
BEGIN SELECT RAISE(ABORT,'Playlist source does not belong to owner'); END;

-- 旧的本地条目是指定来源引用；远程资料、绑定和历史快照继续保留。
INSERT INTO music_playlist_entries(owner,playlist_id,binding_id,entry_id,track_id,forced_source_id,position,created_at,updated_at)
SELECT p.owner,p.id,json_extract(b.value,'$.bindingId'),json_extract(e.value,'$.entryId'),s.track_id,s.id,e.key,
 coalesce(json_extract(p.document,'$.createdAt'),strftime('%Y-%m-%dT%H:%M:%fZ','now')),
 coalesce(json_extract(p.document,'$.updatedAt'),strftime('%Y-%m-%dT%H:%M:%fZ','now'))
FROM personal_playlists p,json_each(p.document,'$.bindings') b,json_each(b.value,'$.entries') e
JOIN music_sources s ON s.owner=p.owner AND s.id=json_extract(e.value,'$.source.sourceTrackId')
WHERE json_extract(b.value,'$.kind')='local';

-- 历史快照按原修订留存。迁移后的当前绑定和隐藏规则在下一次重建时使用规范曲目。
UPDATE personal_playlists SET rule_revision=rule_revision+1,
 document=json_set(document,'$.ruleRevision',rule_revision+1,'$.stale',json('true'))
 WHERE EXISTS(SELECT 1 FROM music_playlist_entries e WHERE e.owner=personal_playlists.owner AND e.playlist_id=personal_playlists.id);
UPDATE playlist_hidden_rules AS h
 SET document=json_set(h.document,'$.Rule.source',json_object('trackId',s.track_id,
  'forcedSourceTrackId',s.id,'sourceTrackId',NULL,'remoteSourceId',NULL),'$.Allowed',json((
   SELECT json_group_array(json(CASE WHEN m.id IS NULL THEN a.value ELSE json_object('trackId',m.track_id,
    'forcedSourceTrackId',m.id,'sourceTrackId',NULL,'remoteSourceId',NULL) END))
   FROM json_each(h.document,'$.Allowed') a LEFT JOIN music_sources m ON m.owner=h.owner AND m.id=json_extract(a.value,'$.sourceTrackId')
  )))
 FROM music_sources s WHERE s.owner=h.owner AND s.id=json_extract(h.document,'$.Rule.source.sourceTrackId');

-- 单来源曲目首次并入录音时，歌单仍指向同一首曲子。拆出指定版本则保留原选择，供界面提示失效。
CREATE TRIGGER music_playlist_group_tracks AFTER UPDATE OF track_id ON music_sources
WHEN OLD.id=OLD.track_id AND NEW.track_id<>OLD.track_id BEGIN
 UPDATE music_playlist_entries SET track_id=NEW.track_id,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
 WHERE owner=NEW.owner AND binding_id<>'' AND track_id=OLD.track_id;
 UPDATE playlist_hidden_rules SET document=json_set(document,'$.Rule.source.trackId',NEW.track_id,
  '$.Allowed',json((SELECT json_group_array(json(CASE WHEN json_extract(a.value,'$.trackId')=OLD.track_id
   THEN json_set(a.value,'$.trackId',NEW.track_id) ELSE a.value END)) FROM json_each(document,'$.Allowed') a)))
 WHERE owner=NEW.owner AND json_extract(document,'$.Rule.source.trackId')=OLD.track_id;
END;

CREATE TRIGGER music_playlist_regroup_contribution AFTER UPDATE OF track_id ON music_sources
WHEN NEW.track_id<>OLD.track_id BEGIN
 UPDATE music_playlist_entries SET track_id=NEW.track_id,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
 WHERE owner=NEW.owner AND binding_id<>'' AND source_contribution=1 AND forced_source_id=NEW.id AND track_id<>NEW.track_id;
END;

-- 收藏按曲目去重，最近播放按实际来源保留版本和最近一次开始播放的时间。
INSERT INTO music_playlist_entries(owner,playlist_id,binding_id,entry_id,track_id,forced_source_id,position,created_at,updated_at)
 SELECT owner,'favorites','',track_id,track_id,source_id,0,created_at,created_at FROM (
 SELECT f.owner,s.track_id,s.id AS source_id,f.created_at,row_number() OVER(PARTITION BY f.owner,s.track_id ORDER BY f.created_at DESC,s.id) AS n
 FROM personal_favorites f JOIN music_sources s ON s.owner=f.owner AND s.id=f.source_track_id
 ) WHERE n=1;
INSERT INTO music_playlist_entries(owner,playlist_id,binding_id,entry_id,track_id,forced_source_id,position,created_at,updated_at,play_count)
 SELECT r.owner,'recently-played','',s.id,s.track_id,s.id,0,r.played_at,r.played_at,1
 FROM personal_recently_played r JOIN music_sources s ON s.owner=r.owner AND s.id=r.source_track_id;
DROP TABLE personal_favorites;
DROP TABLE personal_recently_played;
-- 宿主仅保留尚未交付的事件。插件持久化成功后确认，旧记录也由相同的插件规则接管。
CREATE TABLE content_history_pending (
 owner TEXT NOT NULL, type_id TEXT NOT NULL, event_id TEXT NOT NULL, work_id TEXT NOT NULL,
 work_title TEXT NOT NULL, source_id TEXT NOT NULL, track_title TEXT NOT NULL, recorded_at TEXT NOT NULL,
 PRIMARY KEY(owner,type_id,event_id)
);
CREATE INDEX content_history_delivery ON content_history_pending(type_id,owner,recorded_at,event_id);
WITH candidates AS (
 SELECT e.owner,e.entry_id,e.updated_at,w.content_type,w.id AS work_id,w.title AS work_title,m.title AS track_title,
 row_number() OVER (PARTITION BY e.owner,e.entry_id ORDER BY
  (json_extract(n.value,'$.sourceTrackId')=s.id) DESC,
  CASE w.content_type WHEN 'cd_album' THEN 0 WHEN 'asmr' THEN 1 ELSE 2 END,w.id) AS choice
 FROM music_playlist_entries e JOIN tracks s ON s.id=e.forced_source_id AND s.owner=e.owner
 JOIN music_tracks m ON m.id=e.track_id AND m.owner=e.owner
 JOIN work_packages p ON p.owner=e.owner JOIN works w ON w.id=p.work_id AND w.owner=p.owner
 JOIN json_each(p.document,'$.nodes') n
 WHERE e.playlist_id='recently-played' AND e.binding_id='' AND w.content_type IN ('cd_album','asmr','radio')
 AND json_extract(n.value,'$.kind')='audio' AND (json_extract(n.value,'$.sourceTrackId')=s.id
 OR (p.provider_id=s.provider AND p.root_id=s.root_id AND json_extract(n.value,'$.sourcePath')=s.path))
)
INSERT INTO content_history_pending SELECT owner,content_type,'migration:'||entry_id,work_id,work_title,entry_id,track_title,updated_at
FROM candidates WHERE choice=1;
DELETE FROM music_playlist_entries AS e WHERE e.playlist_id='recently-played' AND e.binding_id=''
AND EXISTS(SELECT 1 FROM content_history_pending p WHERE p.owner=e.owner AND p.source_id=e.entry_id);
ALTER TABLE personal_playback_receipts ADD COLUMN history_target TEXT;
CREATE INDEX music_special_playlist_order ON music_playlist_entries(owner,playlist_id,updated_at DESC,entry_id) WHERE binding_id='';
UPDATE personal_playlists SET document=json_set(document,'$.kind','user');
-- 归并最后一个来源时同步迁移自动建档曲目的收藏。手工曲目即使失去所有来源也保留。


-- 特殊歌单的生命周期属于账号。即使为空，创建时间、ID 和版本仍可读取。
ALTER TABLE personal_collection_revisions RENAME TO previous_collection_revisions;
CREATE TABLE personal_collection_revisions (
 owner TEXT NOT NULL,kind TEXT NOT NULL,revision INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')),
 updated_at TEXT NOT NULL DEFAULT(strftime('%Y-%m-%dT%H:%M:%fZ','now')),
 PRIMARY KEY(owner,kind)
);
INSERT INTO personal_collection_revisions(owner,kind,revision) SELECT owner,kind,revision FROM previous_collection_revisions;
DROP TABLE previous_collection_revisions;
INSERT OR IGNORE INTO personal_collection_revisions(owner,kind) SELECT id,'favorites' FROM users;
INSERT OR IGNORE INTO personal_collection_revisions(owner,kind) SELECT id,'recently-played' FROM users;
CREATE TRIGGER music_new_user_playlists AFTER INSERT ON users BEGIN
 INSERT OR IGNORE INTO personal_collection_revisions(owner,kind) VALUES(NEW.id,'favorites'),(NEW.id,'recently-played');
END;
CREATE TRIGGER music_playlist_version AFTER UPDATE OF revision ON personal_collection_revisions BEGIN
 UPDATE personal_collection_revisions SET updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE owner=NEW.owner AND kind=NEW.kind;
END;

CREATE TRIGGER music_special_playlist_regroup AFTER UPDATE OF track_id ON music_sources
WHEN OLD.track_id<>NEW.track_id BEGIN
 UPDATE music_playlist_entries SET track_id=NEW.track_id WHERE owner=NEW.owner AND playlist_id='recently-played' AND binding_id='' AND forced_source_id=NEW.id;
 UPDATE personal_collection_revisions SET revision=revision+1 WHERE owner=OLD.owner AND kind='recently-played'
 AND EXISTS(SELECT 1 FROM music_playlist_entries WHERE owner=OLD.owner AND playlist_id='recently-played' AND binding_id='' AND forced_source_id=NEW.id);
 UPDATE personal_collection_revisions SET revision=revision+1 WHERE owner=OLD.owner AND kind='favorites'
 AND EXISTS(SELECT 1 FROM music_playlist_entries WHERE owner=OLD.owner AND playlist_id='favorites' AND binding_id='' AND track_id=OLD.track_id);
 INSERT OR IGNORE INTO music_playlist_entries(owner,playlist_id,binding_id,entry_id,track_id,forced_source_id,position,created_at,updated_at)
 SELECT owner,'favorites','',NEW.track_id,NEW.track_id,forced_source_id,position,created_at,updated_at FROM music_playlist_entries p
 WHERE p.owner=OLD.owner AND p.playlist_id='favorites' AND p.binding_id='' AND p.track_id=OLD.track_id
 AND NOT EXISTS(SELECT 1 FROM music_sources WHERE track_id=OLD.track_id)
 AND EXISTS(SELECT 1 FROM music_tracks WHERE id=OLD.track_id AND origin<>'manual');
 DELETE FROM music_playlist_entries WHERE owner=OLD.owner AND playlist_id='favorites' AND binding_id='' AND track_id=OLD.track_id
 AND NOT EXISTS(SELECT 1 FROM music_sources WHERE track_id=OLD.track_id)
 AND EXISTS(SELECT 1 FROM music_tracks WHERE id=OLD.track_id AND origin<>'manual');
END;
-- migration: 039_favorite_regroup_conflict.sql
-- 归并多个已收藏来源时显式处理同一规范曲目的冲突，避免继承外层 UPSERT 的策略。
DROP TRIGGER music_special_playlist_regroup;
CREATE TRIGGER music_special_playlist_regroup AFTER UPDATE OF track_id ON music_sources
WHEN OLD.track_id<>NEW.track_id BEGIN
 UPDATE music_playlist_entries SET track_id=NEW.track_id WHERE owner=NEW.owner AND playlist_id='recently-played' AND binding_id='' AND forced_source_id=NEW.id;
 UPDATE personal_collection_revisions SET revision=revision+1 WHERE owner=OLD.owner AND kind='recently-played'
 AND EXISTS(SELECT 1 FROM music_playlist_entries WHERE owner=OLD.owner AND playlist_id='recently-played' AND binding_id='' AND forced_source_id=NEW.id);
 UPDATE personal_collection_revisions SET revision=revision+1 WHERE owner=OLD.owner AND kind='favorites'
 AND EXISTS(SELECT 1 FROM music_playlist_entries WHERE owner=OLD.owner AND playlist_id='favorites' AND binding_id='' AND track_id=OLD.track_id);
 INSERT INTO music_playlist_entries(owner,playlist_id,binding_id,entry_id,track_id,forced_source_id,position,created_at,updated_at)
 SELECT owner,'favorites','',NEW.track_id,NEW.track_id,forced_source_id,position,created_at,updated_at FROM music_playlist_entries p
 WHERE p.owner=OLD.owner AND p.playlist_id='favorites' AND p.binding_id='' AND p.track_id=OLD.track_id
 AND NOT EXISTS(SELECT 1 FROM music_sources WHERE track_id=OLD.track_id)
 AND EXISTS(SELECT 1 FROM music_tracks WHERE id=OLD.track_id AND origin<>'manual')
 ON CONFLICT(owner,playlist_id,binding_id,entry_id) DO NOTHING;
 DELETE FROM music_playlist_entries WHERE owner=OLD.owner AND playlist_id='favorites' AND binding_id='' AND track_id=OLD.track_id
 AND NOT EXISTS(SELECT 1 FROM music_sources WHERE track_id=OLD.track_id)
 AND EXISTS(SELECT 1 FROM music_tracks WHERE id=OLD.track_id AND origin<>'manual');
END;
-- migration: 040_music_album_scraped.sql
-- 专辑刮削资料按字段保存来源和内容，生命周期跟随音乐专辑。
CREATE TABLE music_album_scraped_fields (
 album_id TEXT NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
 field TEXT NOT NULL CHECK(field IN ('albumDetails','albumTracks','artwork')),
 document TEXT NOT NULL CHECK(json_valid(document)),
 PRIMARY KEY(album_id,field)
);
-- migration: 041_album_original_name.sql
-- 专辑显示名可人工修改；原名在首次创建时确定并保留。
ALTER TABLE music_albums ADD COLUMN original_name TEXT NOT NULL DEFAULT '';
UPDATE music_albums SET original_name=name;
CREATE TRIGGER music_album_initial_original_name AFTER INSERT ON music_albums
WHEN NEW.original_name='' BEGIN
 UPDATE music_albums SET original_name=NEW.name WHERE id=NEW.id;
END;
-- 旧 iTunes 描述字段保存的实际是音乐风格，迁入明确字段。
UPDATE music_album_scraped_fields SET document=json_remove(
 json_set(document,'$.item.genre',json_extract(document,'$.item.description')),'$.item.description')
WHERE json_extract(document,'$.pluginId')='musicfs.scraper.itunes'
 AND json_extract(document,'$.item.genre') IS NULL
 AND length(trim(coalesce(json_extract(document,'$.item.description'),'')))>0;

-- 刮削曲目通过发行版内的位置保持身份；资料手工改名后重刮仍复用曲目。
CREATE TABLE music_album_scraped_track_refs (
 album_id TEXT NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
 slot TEXT NOT NULL, track_id TEXT NOT NULL REFERENCES music_tracks(id) ON DELETE CASCADE,
 plugin_id TEXT NOT NULL, release_id TEXT NOT NULL,
 position INTEGER NOT NULL CHECK(position>=0),
 PRIMARY KEY(album_id,slot), UNIQUE(album_id,track_id)
);
CREATE TRIGGER music_scraped_album_track_owner BEFORE INSERT ON music_album_scraped_track_refs
WHEN (SELECT owner FROM music_albums WHERE id=NEW.album_id)<>(SELECT owner FROM music_tracks WHERE id=NEW.track_id)
BEGIN SELECT RAISE(ABORT,'Album and scraped track owners differ'); END;
DROP VIEW music_album_tracks;
CREATE VIEW music_album_tracks AS
 SELECT album_id,track_id,coalesce(
  (SELECT r.position FROM music_album_scraped_track_refs r WHERE r.album_id=members.album_id AND r.track_id=members.track_id),
  (SELECT max(r.position)+1 FROM music_album_scraped_track_refs r WHERE r.album_id=members.album_id)+min(position),
  min(position)) AS position
 FROM (
  SELECT album_id,track_id,position FROM music_manual_album_tracks
  UNION ALL SELECT a.album_id,s.track_id,a.position FROM music_album_source_refs a
   JOIN music_sources s ON s.id=a.source_id JOIN music_albums b ON b.id=a.album_id AND b.owner=s.owner
 ) members GROUP BY album_id,track_id;
-- migration: 042_album_source_link.sql
-- 专辑页面关联独立保存；保存关联本身不执行网络请求。
ALTER TABLE music_albums ADD COLUMN source_url TEXT;
ALTER TABLE music_albums ADD COLUMN source_plugin_id TEXT;
UPDATE music_albums SET
 source_url=(SELECT json_extract(f.document,'$.item.sourceUrl') FROM music_album_scraped_fields f WHERE f.album_id=music_albums.id AND f.field='albumDetails'),
 source_plugin_id=(SELECT json_extract(f.document,'$.pluginId') FROM music_album_scraped_fields f WHERE f.album_id=music_albums.id AND f.field='albumDetails')
WHERE EXISTS(SELECT 1 FROM music_album_scraped_fields f WHERE f.album_id=music_albums.id AND f.field='albumDetails'
 AND json_extract(f.document,'$.item.sourceUrl') LIKE 'https://%'
 AND length(coalesce(json_extract(f.document,'$.pluginId'),''))>0);
-- migration: 043_localized_music_names.sql
-- 曲目的显示名保留在 title，同语言可以有多个不同的别名。
ALTER TABLE music_tracks ADD COLUMN localized_titles TEXT NOT NULL DEFAULT '[]'
 CHECK(json_valid(localized_titles) AND json_type(localized_titles)='array');
ALTER TABLE music_tracks ADD COLUMN scraper_placeholder INTEGER NOT NULL DEFAULT 0
 CHECK(scraper_placeholder IN (0,1));
ALTER TABLE music_album_scraped_track_refs ADD COLUMN external_id TEXT NOT NULL DEFAULT '';

ALTER TABLE music_artists ADD COLUMN names_revision INTEGER NOT NULL DEFAULT 1;
CREATE TABLE music_artist_localized_names (
 artist_id TEXT NOT NULL REFERENCES music_artists(id) ON DELETE CASCADE,
 language TEXT NOT NULL, name TEXT NOT NULL,
 PRIMARY KEY(artist_id,language,name)
);
CREATE INDEX music_artist_alias_name ON music_artist_localized_names(name COLLATE NOCASE,artist_id);

-- 保留手工关联优先，并从旧版各选用字段补回其他平台页面。
ALTER TABLE music_albums ADD COLUMN source_links TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(source_links) AND json_type(source_links)='array');
WITH links AS (
 SELECT id AS album_id,source_plugin_id AS plugin,source_url AS url,0 AS priority FROM music_albums WHERE source_url IS NOT NULL
 UNION ALL SELECT album_id,json_extract(document,'$.pluginId'),json_extract(document,'$.item.sourceUrl'),
 CASE field WHEN 'albumDetails' THEN 1 WHEN 'albumTracks' THEN 2 ELSE 3 END FROM music_album_scraped_fields
), ranked AS (
 SELECT *,row_number() OVER(PARTITION BY album_id,plugin ORDER BY priority) AS rank FROM links WHERE plugin<>'' AND url LIKE 'https://%'
)
UPDATE music_albums SET source_links=(SELECT json_group_array(json_object('pluginId',plugin,'url',url))
 FROM (SELECT plugin,url FROM ranked WHERE album_id=music_albums.id AND rank=1 ORDER BY priority,plugin));
ALTER TABLE music_albums DROP COLUMN source_url;
ALTER TABLE music_albums DROP COLUMN source_plugin_id;

-- 利用旧刮削事务回执的写入时刻识别自动补建的行。手工创建、修改过或身份不明的行不参与自动清理。
CREATE TEMP TABLE old_scraper_rows AS
 SELECT r.album_id,r.track_id,r.position,m.owner,m.title,j.value AS item,
 coalesce(nullif(trim(json_extract(j.value,'$.artist')),''),json_extract(f.document,'$.item.artist')) AS credit
 FROM music_album_scraped_track_refs r JOIN music_album_scraped_fields f ON f.album_id=r.album_id AND f.field='albumTracks'
 JOIN json_each(f.document,'$.item.tracks') j ON j.key=r.position
 JOIN music_tracks m ON m.id=r.track_id AND m.title=json_extract(j.value,'$.title')
 WHERE m.origin='manual' AND m.revision=1 AND m.metadata_locked=1 AND m.metadata_source_id IS NULL
 AND m.genre IS NULL AND m.cover_path IS NULL AND m.year IS NULL AND m.rating IS NULL AND m.created_at=m.updated_at
 AND json_extract(f.document,'$.pluginId')=r.plugin_id AND json_extract(f.document,'$.item.id')=r.release_id
 AND NOT EXISTS (SELECT 1 FROM music_sources WHERE track_id=m.id)
 AND NOT EXISTS (SELECT 1 FROM idempotency WHERE owner=m.owner AND route='music/tracks/' AND json_extract(result,'$.trackId')=m.id)
 AND EXISTS (SELECT 1 FROM idempotency WHERE owner=m.owner AND route IN ('music/albums/scraped/'||r.album_id,'music/albums/metadata-merge/'||r.album_id)
 AND abs(julianday(expires_at,'-1 day')-julianday(m.created_at))*86400 < 2);
UPDATE music_tracks SET scraper_placeholder=1 WHERE id IN(SELECT track_id FROM old_scraper_rows);

-- 基于专辑内不同来源的全部时长及完整署名双向唯一匹配。不同版本标记和已有用户引用留给人工确认。
CREATE TEMP TABLE old_album_members AS
 SELECT x.album_id,x.track_id,m.title,min(json_extract(t.document,'$.durationMs')) AS shortest,max(json_extract(t.document,'$.durationMs')) AS longest
 FROM (SELECT DISTINCT album_id FROM old_scraper_rows) p JOIN music_album_tracks x ON x.album_id=p.album_id
 JOIN music_tracks m ON m.id=x.track_id JOIN music_sources s ON s.track_id=m.id AND s.owner=m.owner JOIN tracks t ON t.id=s.id AND t.owner=s.owner
 GROUP BY x.album_id,x.track_id HAVING shortest>0 AND count(*)=count(json_extract(t.document,'$.durationMs'));
CREATE TEMP TABLE old_album_matches AS
 WITH RECURSIVE split(track_id,rest,name) AS (
  SELECT track_id,replace(replace(credit,'×','/'),'、','/')||'/', '' FROM old_scraper_rows
  UNION ALL SELECT track_id,substr(rest,instr(rest,'/')+1),lower(trim(substr(rest,1,instr(rest,'/')-1))) FROM split WHERE rest<>''
 ), pairs AS (
  SELECT p.*,s.track_id AS target
  FROM old_scraper_rows p JOIN old_album_members s ON p.album_id=s.album_id
  WHERE p.credit<>'' AND json_extract(p.item,'$.duration')>0
   AND abs(s.shortest-1000*json_extract(p.item,'$.duration'))<=1000 AND abs(s.longest-1000*json_extract(p.item,'$.duration'))<=1000
   AND NOT EXISTS (SELECT name FROM split WHERE track_id=p.track_id AND name<>'' EXCEPT
    SELECT lower(trim(a.name)) FROM music_track_artists c JOIN music_artists a ON a.id=c.artist_id WHERE c.track_id=s.track_id)
   AND NOT EXISTS (SELECT lower(trim(a.name)) FROM music_track_artists c JOIN music_artists a ON a.id=c.artist_id WHERE c.track_id=s.track_id EXCEPT
    SELECT name FROM split WHERE track_id=p.track_id AND name<>'')
   AND NOT EXISTS (SELECT 1 FROM json_each('["live","remix","instrumental","karaoke","acoustic","remaster","version","伴奏","现场","カラオケ"]')
    WHERE (instr(lower(p.title),value)>0)<>(instr(lower(s.title),value)>0))
 ), ranked AS (
  SELECT *,count(*) OVER(PARTITION BY track_id) AS targets,count(*) OVER(PARTITION BY album_id,target) AS candidates FROM pairs
 )
 SELECT album_id,track_id,target,position,title FROM ranked r WHERE targets=1 AND candidates=1
 AND NOT EXISTS(SELECT 1 FROM music_album_scraped_track_refs WHERE album_id=r.album_id AND track_id=r.target)
 AND NOT EXISTS(SELECT 1 FROM music_manual_album_tracks WHERE track_id=r.track_id AND album_id<>r.album_id)
 AND NOT EXISTS(SELECT 1 FROM music_playlist_entries WHERE track_id=r.track_id)
 AND NOT EXISTS(SELECT 1 FROM recording_source_links WHERE recording_id=r.track_id);
UPDATE music_tracks SET localized_titles=(SELECT json_group_array(json_object('language','und','title',title))
 FROM (SELECT DISTINCT title FROM old_album_matches WHERE target=music_tracks.id AND title<>music_tracks.title)),
 revision=revision+1 WHERE id IN(SELECT target FROM old_album_matches);
INSERT INTO music_manual_album_tracks(album_id,track_id,position) SELECT album_id,target,position FROM old_album_matches WHERE true
 ON CONFLICT(album_id,track_id) DO UPDATE SET position=excluded.position;
UPDATE music_album_scraped_track_refs AS r SET track_id=m.target FROM old_album_matches m WHERE r.album_id=m.album_id AND r.track_id=m.track_id;
DELETE FROM music_tracks WHERE scraper_placeholder=1 AND id IN(SELECT track_id FROM old_album_matches);
UPDATE music_albums SET revision=revision+1 WHERE id IN(SELECT album_id FROM old_album_matches);
UPDATE library_revision SET revision=revision+1 WHERE EXISTS(SELECT 1 FROM old_album_matches);
DROP TABLE old_album_matches;
DROP TABLE old_album_members;
DROP TABLE old_scraper_rows;

-- 手动合并仅影响指定发行版的曲位。保留被折叠的上游编号，以便重刮时复用同一曲目。
CREATE TABLE music_album_merged_slots (
 album_id TEXT NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
 plugin_id TEXT NOT NULL, release_id TEXT NOT NULL, slot_key TEXT NOT NULL,
 track_id TEXT NOT NULL REFERENCES music_tracks(id) ON DELETE CASCADE,
 preferred INTEGER NOT NULL CHECK(preferred IN(0,1)),
 PRIMARY KEY(album_id,plugin_id,release_id,slot_key)
);
CREATE INDEX music_album_merged_slots_track ON music_album_merged_slots(track_id);
-- migration: 044_playlist_links.sql
-- 即使不同 MusicFS 用户或实例登录同一平台账号，一个平台歌单也只对应一个本地歌单。
CREATE TABLE playlist_links (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 owner TEXT NOT NULL, platform TEXT NOT NULL, instance_id TEXT NOT NULL,
 account_id TEXT NOT NULL, external_id TEXT NOT NULL,
 playlist_id TEXT NOT NULL REFERENCES personal_playlists(id) ON DELETE CASCADE,
 UNIQUE(platform,external_id)
);
CREATE INDEX playlist_links_local ON playlist_links(owner,playlist_id);

-- 歌曲追加和待发送记录在同一事务内提交。解除关联会清除尚未发送的记录。
CREATE TABLE playlist_link_additions (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 link_id INTEGER NOT NULL REFERENCES playlist_links(id) ON DELETE CASCADE,
 track_id TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, retry_at INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '', UNIQUE(link_id,track_id)
);
CREATE INDEX playlist_link_additions_ready ON playlist_link_additions(retry_at,id);
-- migration: 045_recommendation_pages.sql
-- 按用户、来源组合和登录代次保存整页推荐；递增编号区分同一时刻的多次刷新。
CREATE TABLE recommendation_pages (
    id INTEGER PRIMARY KEY,
    owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scope TEXT NOT NULL,
    server_day TEXT NOT NULL,
    successful INTEGER NOT NULL CHECK (successful IN (0, 1)),
    sources_json TEXT NOT NULL
);
CREATE INDEX recommendation_page_history ON recommendation_pages(owner_id, scope, id DESC);
-- migration: 046_favorite_playlist_links.sql
-- 自建歌单保留删除级联；空外键表示该用户独立的系统收藏。
ALTER TABLE playlist_links RENAME TO previous_playlist_links;
ALTER TABLE playlist_link_additions RENAME TO previous_playlist_link_additions;

CREATE TABLE playlist_links (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 owner TEXT NOT NULL, platform TEXT NOT NULL, instance_id TEXT NOT NULL,
 account_id TEXT NOT NULL, external_id TEXT NOT NULL,
 personal_playlist_id TEXT REFERENCES personal_playlists(id) ON DELETE CASCADE,
 playlist_id TEXT GENERATED ALWAYS AS (coalesce(personal_playlist_id,'favorites')) VIRTUAL,
 UNIQUE(platform,external_id)
);
CREATE TABLE playlist_link_additions (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 link_id INTEGER NOT NULL REFERENCES playlist_links(id) ON DELETE CASCADE,
 track_id TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, retry_at INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '', UNIQUE(link_id,track_id)
);

INSERT INTO playlist_links(id,owner,platform,instance_id,account_id,external_id,personal_playlist_id)
 SELECT id,owner,platform,instance_id,account_id,external_id,playlist_id FROM previous_playlist_links;
INSERT INTO playlist_link_additions(id,link_id,track_id,attempts,retry_at,last_error)
 SELECT id,link_id,track_id,attempts,retry_at,last_error FROM previous_playlist_link_additions;

-- 已完成的投递可能已删除，保留两个表的自增序列，避免复用旧编号。
UPDATE sqlite_sequence AS current SET seq=max(seq,coalesce((
 SELECT previous.seq FROM sqlite_sequence previous WHERE previous.name='previous_'||current.name),0))
 WHERE name IN ('playlist_links','playlist_link_additions');
DROP TABLE previous_playlist_link_additions;
DROP TABLE previous_playlist_links;
CREATE INDEX playlist_links_local ON playlist_links(owner,playlist_id);
CREATE INDEX playlist_link_additions_ready ON playlist_link_additions(retry_at,id);
-- migration: 047_online_downloads.sql
-- 每个来源可拥有多份独立下载。索引仅记录已经完整写入并发布的文件。
CREATE TABLE online_downloads (
 id TEXT PRIMARY KEY, lookup_key TEXT NOT NULL, owner TEXT NOT NULL, plugin_id TEXT NOT NULL,
 source_id TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
 document TEXT NOT NULL CHECK(json_valid(document))
);
CREATE INDEX online_downloads_source ON online_downloads(owner,source_id);
-- migration: 048_playlist_catalog_sync.sql
-- 空任务编号表示尚未为此歌单或关联安排曲库入库，启动时由已配置的实例领取。
ALTER TABLE remote_playlists ADD COLUMN catalog_job_id TEXT REFERENCES remote_sync_jobs(id);
ALTER TABLE playlist_links ADD COLUMN catalog_job_id TEXT REFERENCES remote_sync_jobs(id);
