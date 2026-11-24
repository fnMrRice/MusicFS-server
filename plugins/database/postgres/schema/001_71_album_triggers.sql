CREATE FUNCTION music_merge_curated_track_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.track_id=OLD.id AND NEW.track_id<>OLD.track_id THEN
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
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t80_music_merge_curated_track
 AFTER UPDATE OF track_id ON music_sources FOR EACH ROW EXECUTE FUNCTION music_merge_curated_track_fn();

CREATE FUNCTION music_new_source_album_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO music_albums(id,owner,name,origin) VALUES(NEW.id,NEW.owner,NEW.name,'source');
 RETURN NEW;
END;
$$;
CREATE TRIGGER t79_music_new_source_album
 AFTER INSERT ON source_albums FOR EACH ROW EXECUTE FUNCTION music_new_source_album_fn();

CREATE FUNCTION music_changed_source_album_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 UPDATE music_albums SET name=NEW.name,revision=revision+1 WHERE id=NEW.id AND metadata_locked=0 AND name<>NEW.name;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t78_music_changed_source_album
 AFTER UPDATE OF name ON source_albums FOR EACH ROW EXECUTE FUNCTION music_changed_source_album_fn();

CREATE FUNCTION music_removed_source_album_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 DELETE FROM music_albums WHERE id=OLD.id AND origin='source';
 RETURN OLD;
END;
$$;
CREATE TRIGGER t77_music_removed_source_album
 AFTER DELETE ON source_albums FOR EACH ROW EXECUTE FUNCTION music_removed_source_album_fn();

CREATE FUNCTION music_new_source_member_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO music_album_source_refs VALUES(NEW.album_id,NEW.source_id,'source',NEW.position);
 RETURN NEW;
END;
$$;
CREATE TRIGGER t76_music_new_source_member
 AFTER INSERT ON source_album_members FOR EACH ROW EXECUTE FUNCTION music_new_source_member_fn();

CREATE FUNCTION music_changed_source_member_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 DELETE FROM music_album_source_refs WHERE origin_id='source' AND album_id=OLD.album_id AND source_id=OLD.source_id;
  INSERT INTO music_album_source_refs VALUES(NEW.album_id,NEW.source_id,'source',NEW.position);
 RETURN NEW;
END;
$$;
CREATE TRIGGER t75_music_changed_source_member
 AFTER UPDATE ON source_album_members FOR EACH ROW EXECUTE FUNCTION music_changed_source_member_fn();

CREATE FUNCTION music_removed_source_member_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 DELETE FROM music_album_source_refs WHERE origin_id='source' AND album_id=OLD.album_id AND source_id=OLD.source_id;
 RETURN OLD;
END;
$$;
CREATE TRIGGER t74_music_removed_source_member
 AFTER DELETE ON source_album_members FOR EACH ROW EXECUTE FUNCTION music_removed_source_member_fn();

CREATE FUNCTION music_import_cd_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO music_albums(id,owner,name,external_id,is_external,origin)
  SELECT w.id,w.owner,w.title,(SELECT x.value->>'value' FROM jsonb_array_elements(w.document::jsonb->'externalIds') WITH ORDINALITY x(value,ord)
   WHERE x.value->>'namespace'='catalog_number' ORDER BY x.ord LIMIT 1),1,'cd'
  FROM works w WHERE w.id=NEW.id AND w.content_type='cd_album'
  ON CONFLICT(id) DO UPDATE SET name=excluded.name,external_id=excluded.external_id,revision=music_albums.revision+1
  WHERE music_albums.metadata_locked=0 AND (music_albums.name<>excluded.name OR music_albums.external_id IS DISTINCT FROM excluded.external_id);
 RETURN NEW;
END;
$$;
CREATE TRIGGER t73_music_import_cd
 INSTEAD OF INSERT ON music_import_cd_albums FOR EACH ROW EXECUTE FUNCTION music_import_cd_fn();

CREATE FUNCTION music_new_cd_album_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.content_type='cd_album' THEN
  INSERT INTO music_import_cd_albums VALUES(NEW.id);
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t72_music_new_cd_album
 AFTER INSERT ON works FOR EACH ROW EXECUTE FUNCTION music_new_cd_album_fn();

CREATE FUNCTION music_changed_cd_album_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.content_type='cd_album' THEN
  INSERT INTO music_import_cd_albums VALUES(NEW.id);
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t71_music_changed_cd_album
 AFTER UPDATE OF document ON works FOR EACH ROW EXECUTE FUNCTION music_changed_cd_album_fn();

CREATE FUNCTION music_removed_cd_album_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.content_type='cd_album' THEN
  DELETE FROM music_albums WHERE id=OLD.id AND origin='cd';
 END IF;
 RETURN OLD;
END;
$$;
CREATE TRIGGER t70_music_removed_cd_album
 AFTER DELETE ON works FOR EACH ROW EXECUTE FUNCTION music_removed_cd_album_fn();

CREATE FUNCTION music_import_cd_package_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 DELETE FROM music_album_source_refs WHERE origin_id=NEW.id;
  INSERT INTO music_album_source_refs(album_id,source_id,origin_id,position)
  SELECT a.id,s.id,p.id,min(n.ord)-1 FROM work_packages p JOIN music_albums a
  ON a.id=p.work_id AND a.owner=p.owner AND a.origin='cd' CROSS JOIN LATERAL jsonb_array_elements(p.document::jsonb->'nodes') WITH ORDINALITY n(value,ord)
  JOIN music_sources s ON s.id=(n.value->>'sourceTrackId') AND s.owner=p.owner
  WHERE p.id=NEW.id AND (n.value->>'kind')='audio' GROUP BY a.id,s.id,p.id;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t69_music_import_cd_package
 INSTEAD OF INSERT ON music_import_cd_packages FOR EACH ROW EXECUTE FUNCTION music_import_cd_package_fn();

CREATE FUNCTION music_new_cd_package_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO music_import_cd_packages VALUES(NEW.id);
 RETURN NEW;
END;
$$;
CREATE TRIGGER t68_music_new_cd_package
 AFTER INSERT ON work_packages FOR EACH ROW EXECUTE FUNCTION music_new_cd_package_fn();

CREATE FUNCTION music_changed_cd_package_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO music_import_cd_packages VALUES(NEW.id);
 RETURN NEW;
END;
$$;
CREATE TRIGGER t67_music_changed_cd_package
 AFTER UPDATE OF document ON work_packages FOR EACH ROW EXECUTE FUNCTION music_changed_cd_package_fn();

CREATE FUNCTION music_removed_cd_package_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 DELETE FROM music_album_source_refs WHERE origin_id=OLD.id;
 RETURN OLD;
END;
$$;
CREATE TRIGGER t66_music_removed_cd_package
 AFTER DELETE ON work_packages FOR EACH ROW EXECUTE FUNCTION music_removed_cd_package_fn();

CREATE FUNCTION music_set_cover_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 UPDATE music_tracks SET cover_path=u.path,revision=revision+1
  FROM (SELECT (SELECT '/api/v1/media/'||object_id FROM music_track_covers WHERE track_id=NEW.id
   ORDER BY (object_id IS NOT DISTINCT FROM preferred) DESC,width*height DESC,least(width,height) DESC,object_id LIMIT 1) AS path) u
  WHERE music_tracks.id=NEW.id AND music_tracks.metadata_locked=0 AND music_tracks.cover_path IS DISTINCT FROM u.path;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t65_music_set_cover
 INSTEAD OF INSERT ON music_refresh_cover FOR EACH ROW EXECUTE FUNCTION music_set_cover_fn();

CREATE FUNCTION music_track_updated_at_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.revision<>OLD.revision AND NEW.updated_at=OLD.updated_at THEN
  UPDATE music_tracks SET updated_at=musicfs_timestamp() WHERE id=NEW.id;
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t64_music_track_updated_at
 AFTER UPDATE OF revision ON music_tracks FOR EACH ROW EXECUTE FUNCTION music_track_updated_at_fn();

CREATE FUNCTION music_linked_cover_fn() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.track_id<>OLD.track_id THEN
  INSERT INTO music_refresh_cover VALUES(OLD.track_id),(NEW.track_id);
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER t63_music_linked_cover
 AFTER UPDATE OF track_id ON music_sources FOR EACH ROW EXECUTE FUNCTION music_linked_cover_fn();
