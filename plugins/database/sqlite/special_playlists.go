package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

func specialPlaylist(ctx context.Context, tx *sql.Tx, owner, id string) (playlists.Playlist, error) {
	p := playlists.Playlist{ID: id, Kind: id, Name: "Favorites", RuleRevision: 1, Bindings: []playlists.PlaylistBinding{}}
	if id == personal.Recent {
		p.Name = "Recently played"
		if err := pruneRecent(ctx, tx, owner); err != nil {
			return p, err
		}
	}
	// 迁移会为已有账号创建两种歌单，首次读取同样可为新账号初始化。
	_, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO personal_collection_revisions(owner,kind) VALUES(?,?)", owner, id)
	if err != nil {
		return p, err
	}
	var created, updated string
	err = tx.QueryRowContext(ctx, "SELECT revision+1,created_at,updated_at FROM personal_collection_revisions WHERE owner=? AND kind=?", owner, id).Scan(&p.SnapshotRevision, &created, &updated)
	if err != nil {
		return p, err
	}
	if p.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return p, err
	}
	p.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	return p, err
}

func specialSnapshot(ctx context.Context, tx *sql.Tx, owner, id string, revision int64) (playlists.AggregateSnapshot, error) {
	p, err := specialPlaylist(ctx, tx, owner, id)
	snap := playlists.AggregateSnapshot{PlaylistID: id, Revision: p.SnapshotRevision, RuleRevision: p.RuleRevision, CreatedAt: p.UpdatedAt, Dependencies: []playlists.Dependency{}, Entries: []playlists.AggregateEntry{}, NeedsConfirmation: []string{}}
	if err != nil {
		return snap, err
	}
	if snap.Revision != revision {
		return snap, domain.Fail("CURSOR_EXPIRED", "Special playlist changed; reload the current revision")
	}
	snap.MatchingRevision, err = matchingRevision(ctx, tx, owner)
	if err != nil {
		return snap, err
	}
	err = musicRows(ctx, tx, `SELECT entry_id,track_id,forced_source_id FROM music_playlist_entries WHERE owner=? AND playlist_id=? AND binding_id='' ORDER BY updated_at DESC,entry_id`, func(r *sql.Rows) error {
		entry := playlists.AggregateEntry{Position: len(snap.Entries), Provenance: []playlists.Provenance{}}
		if err := r.Scan(&entry.ID, &entry.Source.TrackID, &entry.Source.ForcedSourceID); err != nil {
			return err
		}
		snap.Entries = append(snap.Entries, entry)
		return nil
	}, owner, id)
	return snap, err
}
