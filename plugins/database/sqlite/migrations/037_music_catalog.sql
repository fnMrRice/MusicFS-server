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
