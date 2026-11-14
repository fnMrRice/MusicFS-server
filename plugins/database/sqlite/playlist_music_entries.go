package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

// 曲目与强制来源是不同的身份；修改一个歌单中的版本选择只更新该次出现。
func saveMusicPlaylistEntries(ctx context.Context, tx *sql.Tx, owner string, p playlists.Playlist) error {
	keep := [][2]string{}
	for _, binding := range p.Bindings {
		for pos, entry := range binding.Entries {
			ref := entry.Source
			if ref.RemoteSourceID != nil {
				continue
			}
			track, forced := ref.TrackID, ref.ForcedSourceID
			if ref.SourceTrackID != nil {
				// 聚合来源贡献也记录其规范曲目，以便匹配调整后仍可追踪原出处。
				var id string
				err := tx.QueryRowContext(ctx, "SELECT track_id FROM music_sources WHERE owner=? AND id=?", owner, *ref.SourceTrackID).Scan(&id)
				if err != nil {
					return remoteNotFound(err)
				}
				track, forced = &id, ref.SourceTrackID
			} else if forced != nil {
				var allowed bool
				err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM music_sources
					WHERE owner=? AND id=? AND track_id=?) OR EXISTS(SELECT 1 FROM music_playlist_entries
					WHERE owner=? AND playlist_id=? AND binding_id=? AND entry_id=? AND track_id=? AND forced_source_id=?)`,
					owner, *forced, *track, owner, p.ID, binding.ID, entry.ID, *track, *forced).Scan(&allowed)
				if err != nil {
					return err
				}
				if !allowed {
					return playlists.Invalid()
				}
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO music_playlist_entries(owner,playlist_id,binding_id,entry_id,track_id,forced_source_id,source_contribution,position)
				VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(owner,playlist_id,binding_id,entry_id) DO UPDATE SET
				track_id=excluded.track_id,forced_source_id=excluded.forced_source_id,source_contribution=excluded.source_contribution,
				position=excluded.position,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
				WHERE track_id<>excluded.track_id OR forced_source_id IS NOT excluded.forced_source_id OR source_contribution<>excluded.source_contribution OR position<>excluded.position`,
				owner, p.ID, binding.ID, entry.ID, *track, forced, ref.SourceTrackID != nil, pos)
			if err != nil {
				return err
			}
			keep = append(keep, [2]string{binding.ID, entry.ID})
		}
	}
	encoded, err := json.Marshal(keep)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM music_playlist_entries WHERE owner=? AND playlist_id=? AND binding_id<>''
		AND (binding_id,entry_id) NOT IN (SELECT json_extract(value,'$[0]'),json_extract(value,'$[1]') FROM json_each(?))`, owner, p.ID, string(encoded))
	return err
}

// 绑定与远程出处由现有快照管理；本地曲目的目标和版本选择从条目表读取。
func readMusicPlaylistEntries(ctx context.Context, tx *sql.Tx, owner string, p *playlists.Playlist) error {
	rows, err := tx.QueryContext(ctx, `SELECT binding_id,entry_id,track_id,forced_source_id,source_contribution FROM music_playlist_entries
		WHERE owner=? AND playlist_id=? AND binding_id<>''`, owner, p.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	refs := map[[2]string]playlists.SourceRef{}
	for rows.Next() {
		var binding, entry, track string
		var forced *string
		var contribution bool
		if err = rows.Scan(&binding, &entry, &track, &forced, &contribution); err != nil {
			return err
		}
		ref := playlists.SourceRef{TrackID: &track, ForcedSourceID: forced}
		if contribution {
			ref = playlists.SourceRef{SourceTrackID: forced}
		}
		refs[[2]string{binding, entry}] = ref
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for i := range p.Bindings {
		binding := &p.Bindings[i]
		for j := range binding.Entries {
			entry := &binding.Entries[j]
			if ref, ok := refs[[2]string{binding.ID, entry.ID}]; ok {
				entry.Source = ref
			}
		}
	}
	return nil
}
