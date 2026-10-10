package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/personal"
)

func (s *Store) PersonalProfile(ctx context.Context, owner string) (personal.Profile, error) {
	var p personal.Profile
	err := s.DB.QueryRowContext(ctx, "SELECT u.id,u.role,p.nickname FROM users u LEFT JOIN personal_profiles p ON p.owner=u.id WHERE u.id=?", owner).Scan(&p.User.ID, &p.User.Role, &p.Nickname)
	if errors.Is(err, sql.ErrNoRows) {
		return p, domain.Fail("NOT_FOUND", "User was not found")
	}
	return p, err
}

func (s *Store) SavePersonalProfile(ctx context.Context, owner string, nickname *string) (personal.Profile, error) {
	value, err := personal.ValidateNickname(nickname)
	if err != nil {
		return personal.Profile{}, err
	}
	if _, err = s.PersonalProfile(ctx, owner); err != nil {
		return personal.Profile{}, err
	}
	_, err = s.DB.ExecContext(ctx, "INSERT INTO personal_profiles(owner,nickname) VALUES(?,?) ON CONFLICT(owner) DO UPDATE SET nickname=excluded.nickname", owner, value)
	if err != nil {
		return personal.Profile{}, err
	}
	return s.PersonalProfile(ctx, owner)
}

var _ personal.Repository = (*Store)(nil)
