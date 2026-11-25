-- 同一来源实例的同名标签可能对应不同发行。仅在真实音源的原标签仍匹配时采用逐源归属。
CREATE UNIQUE INDEX IF NOT EXISTS music_album_source_owner_id ON tracks(owner,id);
CREATE TABLE IF NOT EXISTS music_album_source_tag_overrides (
 owner TEXT NOT NULL, source_id TEXT NOT NULL, tag_name TEXT NOT NULL CHECK(length(trim(tag_name))>0),
 album_id TEXT NOT NULL, PRIMARY KEY(owner,source_id,tag_name),
 FOREIGN KEY(owner,source_id) REFERENCES tracks(owner,id) ON DELETE CASCADE,
 FOREIGN KEY(owner,album_id) REFERENCES music_albums(owner,id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS music_album_source_tag_overrides_album ON music_album_source_tag_overrides(owner,album_id);

DROP TRIGGER IF EXISTS music_import_source;
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
 SELECT lower(hex(randomblob(16))),o.owner,o.album,'tag',o.provider,o.album FROM music_observed_tracks o
 WHERE o.id=NEW.id AND o.album IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM music_album_source_tag_overrides s WHERE s.owner=o.owner AND s.source_id=o.id AND s.tag_name=o.album)
 AND NOT EXISTS(
  SELECT 1 FROM music_album_tag_aliases k WHERE k.owner=o.owner AND k.provider=o.provider AND k.tag_name=o.album)
 ON CONFLICT(owner,tag_provider,tag_name) DO NOTHING;
 DELETE FROM music_album_source_refs WHERE source_id=NEW.id AND origin_id='tag';
 INSERT INTO music_album_source_refs(album_id,source_id,origin_id,position)
 SELECT coalesce(s.album_id,k.album_id,a.id),o.id,'tag',0 FROM music_observed_tracks o
 LEFT JOIN music_album_source_tag_overrides s ON s.owner=o.owner AND s.source_id=o.id AND s.tag_name=o.album
 LEFT JOIN music_album_tag_aliases k ON k.owner=o.owner AND k.provider=o.provider AND k.tag_name=o.album
 LEFT JOIN music_albums a ON a.owner=o.owner AND a.tag_provider=o.provider AND a.tag_name=o.album
 WHERE o.id=NEW.id AND coalesce(s.album_id,k.album_id,a.id) IS NOT NULL;
 INSERT INTO music_refresh_metadata SELECT track_id FROM music_observed_tracks WHERE id=NEW.id;
END;
