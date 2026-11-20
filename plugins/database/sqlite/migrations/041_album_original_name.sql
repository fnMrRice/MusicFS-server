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
