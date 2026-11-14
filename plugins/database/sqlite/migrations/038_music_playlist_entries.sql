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
