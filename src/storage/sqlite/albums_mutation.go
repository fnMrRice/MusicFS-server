package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/albums"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

type albumMutation struct {
	tx                        *sql.Tx
	owner, route, key, digest string
	prior                     []byte
}

func (s *Store) beginAlbumMutation(ctx context.Context, owner, route, key string, request any) (*albumMutation, error) {
	if owner == "" || len(key) < 8 || len(key) > 128 {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid album operation")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	m := &albumMutation{tx: tx, owner: owner, route: "albums/" + route, key: key, digest: hex.EncodeToString(sum[:])}
	m.prior, _, err = contentIdempotency(ctx, tx, owner, m.route, key, m.digest)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return m, nil
}
func (m *albumMutation) finish(ctx context.Context, result any) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if err = saveContentIdempotency(ctx, m.tx, m.owner, m.route, m.key, m.digest, raw); err != nil {
		return err
	}
	return m.tx.Commit()
}
func recordAlbumChange(ctx context.Context, tx *sql.Tx, owner string, c albums.Change) error {
	if c.PreviousMembers == nil {
		c.PreviousMembers = []string{}
	}
	if c.Members == nil {
		c.Members = []string{}
	}
	c.CreatedAt = time.Now().UTC()
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "INSERT INTO album_changes(owner,target_kind,target_id,document) VALUES(?,?,?,?)", owner, c.Target.Kind, c.Target.ID, raw)
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
	_, err = tx.ExecContext(ctx, "UPDATE album_changes SET document=? WHERE sequence=?", raw, c.Sequence)
	return err
}
func validateAlbumSources(ctx context.Context, tx *sql.Tx, owner, provider string, ids []string) error {
	for _, id := range ids {
		var actual string
		err := tx.QueryRowContext(ctx, "SELECT provider FROM tracks WHERE owner=? AND id=?", owner, id).Scan(&actual)
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Fail("NOT_FOUND", "Source track not found")
		}
		if err != nil {
			return err
		}
		if actual != provider {
			return domain.Fail("INVALID_ARGUMENT", "Source album members must belong to its provider")
		}
	}
	return nil
}
func replaceAlbumSources(ctx context.Context, tx *sql.Tx, id string, ids []string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM source_album_members WHERE album_id=?", id); err != nil {
		return err
	}
	for position, source := range ids {
		if _, err := tx.ExecContext(ctx, "INSERT INTO source_album_members VALUES(?,?,?)", id, source, position); err != nil {
			return err
		}
	}
	return nil
}
func replaceReleaseAlbums(ctx context.Context, tx *sql.Tx, owner, id string, ids []string) error {
	for _, member := range ids {
		if _, err := albumTargetRevision(ctx, tx, owner, albums.Target{Kind: "sourceAlbum", ID: member}); err != nil {
			return err
		}
		if err := checkReleaseFanout(ctx, tx, member, id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM release_albums WHERE release_id=?", id); err != nil {
		return err
	}
	for position, member := range ids {
		if _, err := tx.ExecContext(ctx, "INSERT INTO release_albums VALUES(?,?,?)", id, member, position); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) AlbumChanges(ctx context.Context, owner string, target albums.Target, after int64, limit int) ([]albums.Change, error) {
	if after < 0 || limit < 1 || limit > albums.MaxPageSize+1 {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid album change page")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = albumTargetRevision(ctx, tx, owner, target); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT document FROM album_changes WHERE owner=? AND target_kind=? AND target_id=? AND sequence>? ORDER BY sequence LIMIT ?", owner, target.Kind, target.ID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []albums.Change{}
	for rows.Next() {
		var raw []byte
		var c albums.Change
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}
