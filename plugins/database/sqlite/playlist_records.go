package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/playlists"
)

func readPersonalPlaylist(ctx context.Context, tx *sql.Tx, owner, id string) (playlists.Playlist, error) {
	var p playlists.Playlist
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT document FROM personal_playlists WHERE owner=? AND id=?", owner, id).Scan(&raw)
	if err != nil {
		return p, remoteNotFound(err)
	}
	err = json.Unmarshal(raw, &p)
	return p, err
}
func savePersonalPlaylist(ctx context.Context, tx *sql.Tx, owner string, p *playlists.Playlist, insert bool) error {
	p.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if insert {
		_, err = tx.ExecContext(ctx, "INSERT INTO personal_playlists VALUES(?,?,?,?,?,?)", p.ID, owner, p.RuleRevision, p.HideRevision, p.SnapshotRevision, raw)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE personal_playlists SET rule_revision=?,hide_revision=?,snapshot_revision=?,document=? WHERE owner=? AND id=?", p.RuleRevision, p.HideRevision, p.SnapshotRevision, raw, owner, p.ID)
	}
	return err
}
func readPersonalSnapshot(ctx context.Context, tx *sql.Tx, owner, id string, revision int64) (playlists.AggregateSnapshot, error) {
	var s playlists.AggregateSnapshot
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT document FROM personal_playlist_snapshots WHERE owner=? AND playlist_id=? AND revision=?", owner, id, revision).Scan(&raw)
	if err != nil {
		return s, remoteNotFound(err)
	}
	err = json.Unmarshal(raw, &s)
	return s, err
}

type playlistMutation struct {
	tx                        *sql.Tx
	owner, route, key, digest string
	prior                     []byte
}

func (s *Store) beginPlaylistMutation(ctx context.Context, owner, route, key string, request any) (*playlistMutation, error) {
	digest, err := playlists.Digest(request)
	if err != nil {
		return nil, err
	}
	tx, err := remoteMutation(ctx, s, owner, key)
	if err != nil {
		return nil, err
	}
	m := &playlistMutation{tx: tx, owner: owner, route: "playlists/" + route, key: key, digest: digest}
	m.prior, _, err = contentIdempotency(ctx, tx, owner, m.route, key, digest)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return m, nil
}
func (m *playlistMutation) finish(ctx context.Context, result any) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if err = saveContentIdempotency(ctx, m.tx, m.owner, m.route, m.key, m.digest, raw); err != nil {
		return err
	}
	return m.tx.Commit()
}
func playlistAudit(ctx context.Context, tx *sql.Tx, owner string, p playlists.Playlist, operation string, ruleID *string) error {
	c := playlists.PlaylistChange{PlaylistID: p.ID, Operation: operation, RuleRevision: p.RuleRevision, HideRevision: p.HideRevision, RuleID: ruleID, CreatedAt: time.Now().UTC()}
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO playlist_changes(owner,playlist_id,document) VALUES(?,?,?)", owner, p.ID, raw)
	if err != nil {
		return err
	}
	c.Sequence, err = result.LastInsertId()
	if err != nil {
		return err
	}
	raw, err = json.Marshal(c)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE playlist_changes SET document=? WHERE sequence=?", raw, c.Sequence)
	return err
}
