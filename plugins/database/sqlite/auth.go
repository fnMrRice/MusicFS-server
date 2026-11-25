package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
)

func (s *Store) SaveAuth(ctx context.Context, a auth.Session) error {
	_, err := s.DB.ExecContext(ctx, "INSERT INTO auth_sessions(id,user_id,csrf,expires_at) VALUES(?,?,?,?)", a.ID, a.User.ID, a.CSRFToken, a.ExpiresAt.Format(time.RFC3339Nano))
	return err
}
func (s *Store) FindAuth(ctx context.Context, id string) (auth.Session, error) {
	a := auth.Session{ID: id}
	var expiry string
	err := s.DB.QueryRowContext(ctx, "SELECT s.user_id,u.role,s.csrf,s.expires_at FROM auth_sessions s JOIN users u ON u.id=s.user_id WHERE s.id=?", id).Scan(&a.User.ID, &a.User.Role, &a.CSRFToken, &expiry)
	if err != nil {
		return a, err
	}
	a.ExpiresAt, err = time.Parse(time.RFC3339Nano, expiry)
	return a, err
}

// UpdateAuthExpiry 只延长仍有效的记录，返回值以数据库原子更新后的截止时间为准。
func (s *Store) UpdateAuthExpiry(ctx context.Context, id string, expiry time.Time) (time.Time, error) {
	deadline := expiry.UTC().Format(time.RFC3339Nano)
	var stored string
	err := s.DB.QueryRowContext(ctx, `UPDATE auth_sessions
		SET expires_at=CASE WHEN julianday(expires_at)<julianday(?) THEN ? ELSE expires_at END
		WHERE id=? AND julianday(expires_at)>julianday('now') RETURNING expires_at`, deadline, deadline, id).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339Nano, stored)
}
func (s *Store) DeleteAuth(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM auth_sessions WHERE id=?", id)
	return err
}
