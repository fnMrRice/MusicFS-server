CREATE FUNCTION music_track_version_cover_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.asset_id<>OLD.asset_id OR NEW.version<>OLD.version
 OR (musicfs_parse_json(NEW.document)->>'availability') IS DISTINCT FROM (musicfs_parse_json(OLD.document)->>'availability') THEN
  INSERT INTO music_refresh_cover SELECT track_id FROM music_sources WHERE id=NEW.id;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t62_music_track_version_cover
 AFTER UPDATE OF document,asset_id,version ON tracks FOR EACH ROW EXECUTE FUNCTION music_track_version_cover_fn();

CREATE FUNCTION music_new_artwork_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.field='artwork' THEN
  INSERT INTO music_refresh_cover SELECT track_id FROM music_sources WHERE id=NEW.source_id;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t61_music_new_artwork
 AFTER INSERT ON media_content_slots FOR EACH ROW EXECUTE FUNCTION music_new_artwork_fn();

CREATE FUNCTION music_changed_artwork_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.field='artwork' THEN
  INSERT INTO music_refresh_cover SELECT track_id FROM music_sources WHERE id=NEW.source_id;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t60_music_changed_artwork
 AFTER UPDATE ON media_content_slots FOR EACH ROW EXECUTE FUNCTION music_changed_artwork_fn();

CREATE FUNCTION music_removed_artwork_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.field='artwork' THEN
  INSERT INTO music_refresh_cover SELECT track_id FROM music_sources WHERE id=OLD.source_id;
 END IF;
 RETURN OLD;
END;
$$;
CREATE TRIGGER t59_music_removed_artwork
 AFTER DELETE ON media_content_slots FOR EACH ROW EXECUTE FUNCTION music_removed_artwork_fn();

CREATE FUNCTION music_selected_artwork_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO music_refresh_cover VALUES(NEW.recording_id);
 RETURN NEW;
END;
$$;
CREATE TRIGGER t58_music_selected_artwork
 AFTER INSERT ON recording_cover_preferences FOR EACH ROW EXECUTE FUNCTION music_selected_artwork_fn();

CREATE FUNCTION music_reselected_artwork_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO music_refresh_cover VALUES(NEW.recording_id);
 RETURN NEW;
END;
$$;
CREATE TRIGGER t57_music_reselected_artwork
 AFTER UPDATE ON recording_cover_preferences FOR EACH ROW EXECUTE FUNCTION music_reselected_artwork_fn();

CREATE FUNCTION music_deselected_artwork_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO music_refresh_cover VALUES(OLD.recording_id);
 RETURN OLD;
END;
$$;
CREATE TRIGGER t56_music_deselected_artwork
 AFTER DELETE ON recording_cover_preferences FOR EACH ROW EXECUTE FUNCTION music_deselected_artwork_fn();

CREATE FUNCTION music_artwork_state_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state<>NEW.state THEN
  INSERT INTO music_refresh_cover SELECT track_id FROM music_sources WHERE id=NEW.source_id;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t55_music_artwork_state
 AFTER UPDATE OF state ON media_content_objects FOR EACH ROW EXECUTE FUNCTION music_artwork_state_fn();

CREATE FUNCTION music_set_album_cover_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 UPDATE music_albums SET cover_path=u.path,revision=revision+1 FROM (
  SELECT (SELECT '/api/v1/media/'||b.object_id FROM album_cover_heads h
  JOIN album_cover_bindings b ON b.id=h.binding_id AND b.owner=h.owner
  JOIN source_albums a ON a.id=h.target_id AND a.owner=h.owner AND a.revision=b.target_revision
  JOIN tracks t ON t.id=b.source_id AND t.owner=b.owner AND t.asset_id=b.asset_id AND t.version=b.asset_version
  JOIN media_content_objects o ON o.id=b.object_id AND o.state='available'
  WHERE h.target_kind='sourceAlbum' AND h.target_id=NEW.id) AS path) u
  WHERE music_albums.id=NEW.id AND music_albums.origin='source' AND music_albums.metadata_locked=0 AND music_albums.cover_path IS DISTINCT FROM u.path;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t54_music_set_album_cover
 INSTEAD OF INSERT ON music_refresh_album_cover FOR EACH ROW EXECUTE FUNCTION music_set_album_cover_fn();

CREATE FUNCTION music_new_album_cover_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.target_kind='sourceAlbum' THEN
  INSERT INTO music_refresh_album_cover VALUES(NEW.target_id);
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t53_music_new_album_cover
 AFTER INSERT ON album_cover_heads FOR EACH ROW EXECUTE FUNCTION music_new_album_cover_fn();

CREATE FUNCTION music_updated_album_cover_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.target_kind='sourceAlbum' THEN
  INSERT INTO music_refresh_album_cover VALUES(NEW.target_id);
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t52_music_updated_album_cover
 AFTER UPDATE ON album_cover_heads FOR EACH ROW EXECUTE FUNCTION music_updated_album_cover_fn();

CREATE FUNCTION music_album_cover_revision_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO music_refresh_album_cover VALUES(NEW.id);
 RETURN NEW;
END;
$$;
CREATE TRIGGER t51_music_album_cover_revision
 AFTER UPDATE OF revision ON source_albums FOR EACH ROW EXECUTE FUNCTION music_album_cover_revision_fn();

CREATE FUNCTION music_album_cover_removed_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.target_kind='sourceAlbum' THEN
  INSERT INTO music_refresh_album_cover VALUES(OLD.target_id);
 END IF;
 RETURN OLD;
END;
$$;
CREATE TRIGGER t50_music_album_cover_removed
 AFTER DELETE ON album_cover_heads FOR EACH ROW EXECUTE FUNCTION music_album_cover_removed_fn();

CREATE FUNCTION music_album_artwork_state_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.state<>NEW.state THEN
  INSERT INTO music_refresh_album_cover SELECT DISTINCT target_id FROM album_cover_bindings WHERE object_id=NEW.id AND target_kind='sourceAlbum';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t49_music_album_artwork_state
 AFTER UPDATE OF state ON media_content_objects FOR EACH ROW EXECUTE FUNCTION music_album_artwork_state_fn();

CREATE FUNCTION music_album_artwork_version_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.asset_id<>OLD.asset_id OR NEW.version<>OLD.version THEN
  INSERT INTO music_refresh_album_cover SELECT DISTINCT target_id FROM album_cover_bindings WHERE source_id=NEW.id AND target_kind='sourceAlbum';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t48_music_album_artwork_version
 AFTER UPDATE OF asset_id,version ON tracks FOR EACH ROW EXECUTE FUNCTION music_album_artwork_version_fn();

CREATE FUNCTION music_playlist_source_owner_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.forced_source_id IS NOT NULL AND NOT EXISTS(
 SELECT 1 FROM music_sources WHERE id=NEW.forced_source_id AND owner=NEW.owner) THEN
  RAISE EXCEPTION 'Playlist source does not belong to owner' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t47_music_playlist_source_owner
 BEFORE INSERT ON music_playlist_entries FOR EACH ROW EXECUTE FUNCTION music_playlist_source_owner_fn();

CREATE FUNCTION music_playlist_updated_source_owner_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.forced_source_id IS NOT NULL AND NOT EXISTS(
 SELECT 1 FROM music_sources WHERE id=NEW.forced_source_id AND owner=NEW.owner) THEN
  RAISE EXCEPTION 'Playlist source does not belong to owner' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t46_music_playlist_updated_source_owner
 BEFORE UPDATE OF owner,forced_source_id ON music_playlist_entries FOR EACH ROW EXECUTE FUNCTION music_playlist_updated_source_owner_fn();

CREATE FUNCTION music_playlist_group_tracks_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.id=OLD.track_id AND NEW.track_id<>OLD.track_id THEN
  UPDATE music_playlist_entries SET track_id=NEW.track_id,updated_at=musicfs_timestamp()
   WHERE owner=NEW.owner AND binding_id<>'' AND track_id=OLD.track_id;
   UPDATE playlist_hidden_rules SET document=jsonb_set(
    jsonb_set(document::jsonb,'{Rule,source,trackId}',to_jsonb(NEW.track_id)),
    '{Allowed}',coalesce((SELECT jsonb_agg(CASE WHEN a.value->>'trackId'=OLD.track_id
      THEN jsonb_set(a.value,'{trackId}',to_jsonb(NEW.track_id)) ELSE a.value END ORDER BY a.ord)
      FROM jsonb_array_elements(document::jsonb->'Allowed') WITH ORDINALITY a(value,ord)), '[]'::jsonb)
   )::text
   WHERE owner=NEW.owner AND document::jsonb #>> '{Rule,source,trackId}'=OLD.track_id;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t45_music_playlist_group_tracks
 AFTER UPDATE OF track_id ON music_sources FOR EACH ROW EXECUTE FUNCTION music_playlist_group_tracks_fn();

CREATE FUNCTION music_playlist_regroup_contribution_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.track_id<>OLD.track_id THEN
  UPDATE music_playlist_entries SET track_id=NEW.track_id,updated_at=musicfs_timestamp()
   WHERE owner=NEW.owner AND binding_id<>'' AND source_contribution=1 AND forced_source_id=NEW.id AND track_id<>NEW.track_id;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t44_music_playlist_regroup_contribution
 AFTER UPDATE OF track_id ON music_sources FOR EACH ROW EXECUTE FUNCTION music_playlist_regroup_contribution_fn();

CREATE FUNCTION music_new_user_playlists_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO personal_collection_revisions(owner,kind) VALUES(NEW.id,'favorites'),(NEW.id,'recently-played') ON CONFLICT DO NOTHING;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t43_music_new_user_playlists
 AFTER INSERT ON users FOR EACH ROW EXECUTE FUNCTION music_new_user_playlists_fn();

CREATE FUNCTION music_playlist_version_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 UPDATE personal_collection_revisions SET updated_at=musicfs_timestamp() WHERE owner=NEW.owner AND kind=NEW.kind;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t42_music_playlist_version
 AFTER UPDATE OF revision ON personal_collection_revisions FOR EACH ROW EXECUTE FUNCTION music_playlist_version_fn();

CREATE FUNCTION music_special_playlist_regroup_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.track_id<>NEW.track_id THEN
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
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t41_music_special_playlist_regroup
 AFTER UPDATE OF track_id ON music_sources FOR EACH ROW EXECUTE FUNCTION music_special_playlist_regroup_fn();

CREATE FUNCTION music_album_initial_original_name_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.original_name='' THEN
  UPDATE music_albums SET original_name=NEW.name WHERE id=NEW.id;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t40_music_album_initial_original_name
 AFTER INSERT ON music_albums FOR EACH ROW EXECUTE FUNCTION music_album_initial_original_name_fn();

CREATE FUNCTION music_scraped_album_track_owner_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF (SELECT owner FROM music_albums WHERE id=NEW.album_id)<>(SELECT owner FROM music_tracks WHERE id=NEW.track_id) THEN
  RAISE EXCEPTION 'Album and scraped track owners differ' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t39_music_scraped_album_track_owner
 BEFORE INSERT ON music_album_scraped_track_refs FOR EACH ROW EXECUTE FUNCTION music_scraped_album_track_owner_fn();
