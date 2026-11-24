CREATE FUNCTION recording_default_sources_member_moved_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.recording_id IS DISTINCT FROM NEW.recording_id THEN
  DELETE FROM recording_default_sources WHERE owner=OLD.owner AND recording_id=OLD.recording_id AND source_id=OLD.source_id;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t99_recording_default_sources_member_moved
 AFTER UPDATE OF recording_id ON recording_source_links FOR EACH ROW EXECUTE FUNCTION recording_default_sources_member_moved_fn();

CREATE FUNCTION music_credit_owner_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF (SELECT owner FROM music_tracks WHERE id=NEW.track_id)<>(SELECT owner FROM music_artists WHERE id=NEW.artist_id) THEN
  RAISE EXCEPTION 'Artist and track owners differ' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t98_music_credit_owner
 BEFORE INSERT ON music_track_artists FOR EACH ROW EXECUTE FUNCTION music_credit_owner_fn();

CREATE FUNCTION music_album_member_owner_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF (SELECT owner FROM music_tracks WHERE id=NEW.track_id)<>(SELECT owner FROM music_albums WHERE id=NEW.album_id) THEN
  RAISE EXCEPTION 'Album and track owners differ' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t97_music_album_member_owner
 BEFORE INSERT ON music_manual_album_tracks FOR EACH ROW EXECUTE FUNCTION music_album_member_owner_fn();

CREATE FUNCTION music_credit_update_owner_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF (SELECT owner FROM music_tracks WHERE id=NEW.track_id)<>(SELECT owner FROM music_artists WHERE id=NEW.artist_id) THEN
  RAISE EXCEPTION 'Artist and track owners differ' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t96_music_credit_update_owner
 BEFORE UPDATE OF track_id,artist_id ON music_track_artists FOR EACH ROW EXECUTE FUNCTION music_credit_update_owner_fn();

CREATE FUNCTION music_album_update_owner_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF (SELECT owner FROM music_tracks WHERE id=NEW.track_id)<>(SELECT owner FROM music_albums WHERE id=NEW.album_id) THEN
  RAISE EXCEPTION 'Album and track owners differ' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t95_music_album_update_owner
 BEFORE UPDATE OF track_id,album_id ON music_manual_album_tracks FOR EACH ROW EXECUTE FUNCTION music_album_update_owner_fn();

CREATE FUNCTION music_source_provider_insert_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 UPDATE music_sources SET plugin_id=NEW.plugin_id,updated_at=musicfs_timestamp()
  WHERE provider_instance_id=NEW.provider_instance_id AND plugin_id IS DISTINCT FROM NEW.plugin_id;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t94_music_source_provider_insert
 AFTER INSERT ON music_source_providers FOR EACH ROW EXECUTE FUNCTION music_source_provider_insert_fn();

CREATE FUNCTION music_source_album_owner_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF (SELECT owner FROM tracks WHERE id=NEW.source_id)<>(SELECT owner FROM music_albums WHERE id=NEW.album_id) THEN
  RAISE EXCEPTION 'Album and source owners differ' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t93_music_source_album_owner
 BEFORE INSERT ON music_album_source_refs FOR EACH ROW EXECUTE FUNCTION music_source_album_owner_fn();

CREATE FUNCTION music_source_album_update_owner_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF (SELECT owner FROM tracks WHERE id=NEW.source_id)<>(SELECT owner FROM music_albums WHERE id=NEW.album_id) THEN
  RAISE EXCEPTION 'Album and source owners differ' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t92_music_source_album_update_owner
 BEFORE UPDATE OF source_id,album_id ON music_album_source_refs FOR EACH ROW EXECUTE FUNCTION music_source_album_update_owner_fn();

CREATE FUNCTION music_import_source_fn() RETURNS trigger
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
  SELECT replace(gen_random_uuid()::text,'-',''),owner,album,'tag',provider,album FROM music_observed_tracks WHERE id=NEW.id AND album IS NOT NULL
  ON CONFLICT(owner,tag_provider,tag_name) DO NOTHING;
  DELETE FROM music_album_source_refs WHERE source_id=NEW.id AND origin_id='tag';
  INSERT INTO music_album_source_refs(album_id,source_id,origin_id,position)
  SELECT a.id,o.id,'tag',0 FROM music_observed_tracks o JOIN music_albums a
  ON a.owner=o.owner AND a.tag_provider=o.provider AND a.tag_name=o.album WHERE o.id=NEW.id;
  INSERT INTO music_refresh_metadata SELECT track_id FROM music_observed_tracks WHERE id=NEW.id;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t91_music_import_source
 INSTEAD OF INSERT ON music_import_sources FOR EACH ROW EXECUTE FUNCTION music_import_source_fn();

CREATE FUNCTION music_refresh_track_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 UPDATE music_tracks SET metadata_source_id=(SELECT s.id FROM music_sources s WHERE s.owner=music_tracks.owner
   AND s.track_id=music_tracks.id ORDER BY s.id LIMIT 1)
  WHERE id=NEW.id AND origin<>'manual' AND NOT EXISTS(SELECT 1 FROM music_sources s WHERE s.id=metadata_source_id AND s.track_id=NEW.id);
  UPDATE music_tracks SET (title,genre,year)=(SELECT o.title,o.genre,o.year FROM music_observed_tracks o WHERE o.id=metadata_source_id),
   revision=revision+1
  WHERE id=NEW.id AND origin<>'manual' AND metadata_locked=0 AND EXISTS(SELECT 1 FROM music_observed_tracks o
   WHERE o.id=metadata_source_id AND (o.title IS DISTINCT FROM music_tracks.title OR o.genre IS DISTINCT FROM music_tracks.genre OR o.year IS DISTINCT FROM music_tracks.year));
  INSERT INTO music_artists(id,owner,name) SELECT replace(gen_random_uuid()::text,'-',''),owner,name FROM music_imported_credits
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
 RETURN NEW;
END;
$$;
CREATE TRIGGER t90_music_refresh_track
 INSTEAD OF INSERT ON music_refresh_metadata FOR EACH ROW EXECUTE FUNCTION music_refresh_track_fn();

CREATE FUNCTION music_provider_renamed_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.provider<>NEW.provider THEN
  INSERT INTO music_source_providers(provider_instance_id,plugin_id)
   SELECT NEW.provider,plugin_id FROM music_source_providers WHERE provider_instance_id=OLD.provider
   ON CONFLICT(provider_instance_id) DO NOTHING;
   UPDATE music_albums SET tag_provider=NEW.provider WHERE tag_provider=OLD.provider AND owner=NEW.owner AND origin='tag';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t89_music_provider_renamed
 BEFORE UPDATE OF provider ON tracks FOR EACH ROW EXECUTE FUNCTION music_provider_renamed_fn();

CREATE FUNCTION music_provider_rename_finished_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.provider<>NEW.provider THEN
  DELETE FROM music_source_providers WHERE provider_instance_id=OLD.provider AND NOT EXISTS(SELECT 1 FROM tracks WHERE provider=OLD.provider);
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t88_music_provider_rename_finished
 AFTER UPDATE OF provider ON tracks FOR EACH ROW EXECUTE FUNCTION music_provider_rename_finished_fn();

CREATE FUNCTION music_new_observation_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO music_import_sources VALUES(NEW.id);
 RETURN NEW;
END;
$$;
CREATE TRIGGER t87_music_new_observation
 AFTER INSERT ON tracks FOR EACH ROW EXECUTE FUNCTION music_new_observation_fn();

CREATE FUNCTION music_changed_observation_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.provider<>NEW.provider OR OLD.external_id<>NEW.external_id
 OR (musicfs_parse_json(OLD.document) IS NULL)<>(musicfs_parse_json(NEW.document) IS NULL)
 OR musicfs_observation_metadata(OLD.document) IS DISTINCT FROM musicfs_observation_metadata(NEW.document) THEN
  INSERT INTO music_import_sources VALUES(NEW.id);
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t86_music_changed_observation
 AFTER UPDATE OF document,provider,external_id ON tracks FOR EACH ROW EXECUTE FUNCTION music_changed_observation_fn();

CREATE FUNCTION music_observation_timestamp_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.version<>NEW.version OR OLD.asset_id<>NEW.asset_id OR OLD.mime<>NEW.mime OR OLD.size<>NEW.size
 OR OLD.root_id<>NEW.root_id OR OLD.path<>NEW.path OR OLD.document IS DISTINCT FROM NEW.document THEN
  UPDATE music_sources SET updated_at=musicfs_timestamp() WHERE id=NEW.id;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t85_music_observation_timestamp
 AFTER UPDATE ON tracks FOR EACH ROW EXECUTE FUNCTION music_observation_timestamp_fn();

CREATE FUNCTION music_asset_timestamp_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.document IS DISTINCT FROM NEW.document OR OLD.current<>NEW.current OR OLD.invalidated_at IS DISTINCT FROM NEW.invalidated_at OR OLD.file_sha512 IS DISTINCT FROM NEW.file_sha512 THEN
  UPDATE music_sources SET updated_at=musicfs_timestamp() WHERE id=NEW.track_id AND owner=NEW.owner;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t84_music_asset_timestamp
 AFTER UPDATE ON source_assets FOR EACH ROW EXECUTE FUNCTION music_asset_timestamp_fn();

CREATE FUNCTION music_new_recording_link_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO music_import_sources VALUES(NEW.source_id);
 RETURN NEW;
END;
$$;
CREATE TRIGGER t83_music_new_recording_link
 AFTER INSERT ON recording_source_links FOR EACH ROW EXECUTE FUNCTION music_new_recording_link_fn();

CREATE FUNCTION music_changed_recording_link_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.recording_id IS DISTINCT FROM OLD.recording_id THEN
  INSERT INTO music_import_sources VALUES(NEW.source_id);
   INSERT INTO music_refresh_metadata VALUES(coalesce(OLD.recording_id,OLD.source_id));
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t82_music_changed_recording_link
 AFTER UPDATE OF recording_id ON recording_source_links FOR EACH ROW EXECUTE FUNCTION music_changed_recording_link_fn();

CREATE FUNCTION music_removed_recording_link_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO music_import_sources VALUES(OLD.source_id);
  INSERT INTO music_refresh_metadata VALUES(OLD.recording_id);
 RETURN OLD;
END;
$$;
CREATE TRIGGER t81_music_removed_recording_link
 AFTER DELETE ON recording_source_links FOR EACH ROW EXECUTE FUNCTION music_removed_recording_link_fn();
