package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/src/library/music"
)

type albumSlotOverride struct {
	track     string
	preferred bool
}

func albumSlotOverrides(ctx context.Context, tx *sql.Tx, album, plugin, release string) (map[string]albumSlotOverride, error) {
	bindings := map[string]albumSlotOverride{}
	err := musicRows(ctx, tx, `SELECT slot_key,track_id,preferred FROM music_album_merged_slots
 WHERE album_id=? AND plugin_id=? AND release_id=?`, func(r *sql.Rows) error {
		var key string
		var bind albumSlotOverride
		if err := r.Scan(&key, &bind.track, &bind.preferred); err != nil {
			return err
		}
		bindings[key] = bind
		return nil
	}, album, plugin, release)
	return bindings, err
}

// 主曲原有的曲位优先；如果主曲没有平台曲位，就采用勾选曲目中最靠前的位置。
func rememberAlbumMergeSlots(ctx context.Context, tx *sql.Tx, album, target string, in music.MergeTracksInput) error {
	ids := make([]string, 0, len(in.Tracks))
	for _, t := range in.Tracks {
		ids = append(ids, t.TrackID)
	}
	selection, _ := json.Marshal(ids)
	type ref struct{ plugin, release, slot, key string }
	var refs []ref
	err := musicRows(ctx, tx, `SELECT r.plugin_id,r.release_id,r.slot,
 CASE WHEN r.external_id<>'' AND (SELECT count(*) FROM music_album_scraped_track_refs d
 WHERE d.album_id=r.album_id AND d.release_id=r.release_id AND d.plugin_id=r.plugin_id AND d.external_id=r.external_id)=1
 THEN 'id:'||r.external_id ELSE 'slot:'||r.slot END
 FROM music_album_scraped_track_refs r WHERE r.album_id=? AND track_id IN(SELECT value FROM json_each(?))
 ORDER BY (track_id=?) DESC,position,slot`, func(row *sql.Rows) error {
		var r ref
		if err := row.Scan(&r.plugin, &r.release, &r.slot, &r.key); err != nil {
			return err
		}
		refs = append(refs, r)
		return nil
	}, album, string(selection), in.TargetTrackID)
	if err != nil || len(refs) < 2 {
		return err
	}
	for i, r := range refs {
		if _, err = tx.ExecContext(ctx, `UPDATE music_album_merged_slots SET preferred=0
 WHERE album_id=? AND plugin_id=? AND release_id=? AND track_id IN(SELECT value FROM json_each(?))`, album, r.plugin, r.release, string(selection)); err != nil {
			return err
		}
		if i > 0 {
			if _, err = tx.ExecContext(ctx, "DELETE FROM music_album_scraped_track_refs WHERE album_id=? AND slot=?", album, r.slot); err != nil {
				return err
			}
		}
	}
	for i, r := range refs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO music_album_merged_slots(album_id,plugin_id,release_id,slot_key,track_id,preferred)
 VALUES(?,?,?,?,?,?) ON CONFLICT(album_id,plugin_id,release_id,slot_key) DO UPDATE SET track_id=excluded.track_id,preferred=excluded.preferred`, album, r.plugin, r.release, r.key, target, i == 0); err != nil {
			return err
		}
	}
	return nil
}
