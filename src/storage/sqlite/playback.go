package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"time"
)

func (s *Store) PlaybackReset(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE playback_sessions SET state='expired' WHERE state='active'")
	return err
}
func (s *Store) PlaybackSave(ctx context.Context, id, auth, user string, description []byte, state string, expiry time.Time) error {
	_, err := s.DB.ExecContext(ctx, "INSERT INTO playback_sessions VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET description=excluded.description,state=excluded.state", id, auth, user, description, state, expiry.UTC().Format(time.RFC3339Nano))
	return err
}
func (s *Store) PlaybackOwner(ctx context.Context, id string) (string, error) {
	var owner string
	err := s.DB.QueryRowContext(ctx, "SELECT auth_session FROM playback_sessions WHERE id=?", id).Scan(&owner)
	return owner, err
}
func (s *Store) Result(ctx context.Context, owner, route, key, digest string) ([]byte, error) {
	var old, expiry string
	var raw []byte
	err := s.DB.QueryRowContext(ctx, "SELECT digest,result,expires_at FROM idempotency WHERE owner=? AND route=? AND key=?", owner, route, key).Scan(&old, &raw, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if old != digest {
		return nil, domain.Fail("CONFLICT", "Idempotency key has different parameters")
	}
	expiresAt, parseErr := time.Parse(time.RFC3339Nano, expiry)
	if parseErr != nil {
		return nil, domain.Fail("TEMPORARY_FAILURE", "Stored operation expiry is invalid")
	}
	if !time.Now().Before(expiresAt) {
		return nil, domain.Fail("IDEMPOTENCY_EXPIRED", "Operation expired; create a new playback session")
	}
	return raw, nil
}
func (s *Store) SaveResult(ctx context.Context, owner, route, key, digest string, raw []byte) error {
	_, err := s.DB.ExecContext(ctx, "INSERT INTO idempotency VALUES(?,?,?,?,?,?)", owner, route, key, digest, raw, time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano))
	return err
}
func (s *Store) CommitPlayback(ctx context.Context, id, owner, user string, raw []byte, expiry time.Time, route, key, digest string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "INSERT INTO playback_sessions VALUES(?,?,?,?,'active',?) ON CONFLICT(id) DO UPDATE SET description=excluded.description", id, owner, user, raw, expiry.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO idempotency VALUES(?,?,?,?,?,?)", owner, route, key, digest, raw, time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return tx.Commit()
}
