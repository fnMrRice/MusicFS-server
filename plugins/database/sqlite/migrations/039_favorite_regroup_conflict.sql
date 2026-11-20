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
