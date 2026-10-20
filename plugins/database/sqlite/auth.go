package sqlite

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/auth"
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
func (s *Store) DeleteAuth(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM auth_sessions WHERE id=?", id)
	return err
}
