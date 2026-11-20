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
