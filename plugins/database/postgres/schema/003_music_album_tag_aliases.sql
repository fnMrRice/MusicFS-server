-- 显式合并后保留原实例和标签名，后续扫描仍将该来源归入同一张专辑。
CREATE UNIQUE INDEX music_album_owner_id ON music_albums(owner,id);
CREATE TABLE music_album_tag_aliases (
 owner TEXT COLLATE "C" NOT NULL, provider TEXT COLLATE "C" NOT NULL, tag_name TEXT COLLATE "C" NOT NULL, album_id TEXT COLLATE "C" NOT NULL,
 PRIMARY KEY(owner,provider,tag_name), FOREIGN KEY(owner,album_id) REFERENCES music_albums(owner,id) ON DELETE CASCADE
);
CREATE INDEX music_album_tag_aliases_album ON music_album_tag_aliases(album_id);

CREATE OR REPLACE FUNCTION music_import_source_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO music_tracks(id,owner,title,genre,year,origin,metadata_source_id,created_at,updated_at)
  SELECT o.track_id,o.owner,o.title,o.genre,o.year,CASE WHEN o.track_id=o.id THEN 'source' ELSE 'recording' END,o.id,
   coalesce(r.created_at,(SELECT min(a.created_at) FROM source_assets a WHERE a.owner=o.owner AND a.track_id=o.id),
            musicfs_timestamp()),
   coalesce(r.updated_at,(SELECT max(a.created_at) FROM source_assets a WHERE a.owner=o.owner AND a.track_id=o.id),
            musicfs_timestamp())
  FROM music_observed_tracks o LEFT JOIN recordings r ON r.id=o.track_id AND r.owner=o.owner WHERE o.id=NEW.id
  ON CONFLICT(id) DO NOTHING;
  INSERT INTO music_sources(id,owner,track_id,plugin_id,provider_instance_id,external_id,created_at,updated_at)
  SELECT o.id,o.owner,o.track_id,p.plugin_id,o.provider,o.external_id,
   coalesce((SELECT min(a.created_at) FROM source_assets a WHERE a.owner=o.owner AND a.track_id=o.id),musicfs_timestamp()),
   coalesce((SELECT max(a.created_at) FROM source_assets a WHERE a.owner=o.owner AND a.track_id=o.id),musicfs_timestamp())
  FROM music_observed_tracks o LEFT JOIN music_source_providers p ON p.provider_instance_id=o.provider WHERE o.id=NEW.id
  ON CONFLICT(id) DO UPDATE SET track_id=excluded.track_id,plugin_id=excluded.plugin_id,
   provider_instance_id=excluded.provider_instance_id,external_id=excluded.external_id,updated_at=musicfs_timestamp()
  WHERE music_sources.track_id<>excluded.track_id OR music_sources.plugin_id IS DISTINCT FROM excluded.plugin_id
   OR music_sources.provider_instance_id<>excluded.provider_instance_id OR music_sources.external_id<>excluded.external_id;

 INSERT INTO music_albums(id,owner,name,origin,tag_provider,tag_name)
 SELECT replace(gen_random_uuid()::text,'-',''),o.owner,o.album,'tag',o.provider,o.album FROM music_observed_tracks o
 WHERE o.id=NEW.id AND o.album IS NOT NULL AND NOT EXISTS(
  SELECT 1 FROM music_album_tag_aliases k WHERE k.owner=o.owner AND k.provider=o.provider AND k.tag_name=o.album)
 ON CONFLICT(owner,tag_provider,tag_name) DO NOTHING;
 DELETE FROM music_album_source_refs WHERE source_id=NEW.id AND origin_id='tag';
 INSERT INTO music_album_source_refs(album_id,source_id,origin_id,position)
 SELECT coalesce(k.album_id,a.id),o.id,'tag',0 FROM music_observed_tracks o
 LEFT JOIN music_album_tag_aliases k ON k.owner=o.owner AND k.provider=o.provider AND k.tag_name=o.album
 LEFT JOIN music_albums a ON a.owner=o.owner AND a.tag_provider=o.provider AND a.tag_name=o.album
 WHERE o.id=NEW.id AND coalesce(k.album_id,a.id) IS NOT NULL;
 INSERT INTO music_refresh_metadata SELECT track_id FROM music_observed_tracks WHERE id=NEW.id;
 RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION music_provider_renamed_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.provider<>NEW.provider THEN
  INSERT INTO music_source_providers(provider_instance_id,plugin_id)
   SELECT NEW.provider,plugin_id FROM music_source_providers WHERE provider_instance_id=OLD.provider
   ON CONFLICT(provider_instance_id) DO NOTHING;
  DELETE FROM music_album_tag_aliases WHERE owner=NEW.owner AND provider=OLD.provider
  AND EXISTS(SELECT 1 FROM music_album_tag_aliases dest WHERE dest.owner=NEW.owner AND dest.provider=NEW.provider
   AND dest.tag_name=music_album_tag_aliases.tag_name AND dest.album_id=music_album_tag_aliases.album_id);
  UPDATE music_album_tag_aliases SET provider=NEW.provider WHERE owner=NEW.owner AND provider=OLD.provider;
  UPDATE music_albums SET tag_provider=NEW.provider WHERE tag_provider=OLD.provider AND owner=NEW.owner AND origin='tag';
 END IF;
 RETURN NEW;
END;
$$;
