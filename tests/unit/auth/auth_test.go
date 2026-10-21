package auth_test

import (
	"context"
	"errors"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"testing"
	"time"
)

type repository map[string]auth.Session

func (r repository) SaveAuth(_ context.Context, s auth.Session) error { r[s.ID] = s; return nil }
func (r repository) FindAuth(_ context.Context, id string) (auth.Session, error) {
	s, ok := r[id]
	if !ok {
		return s, errors.New("missing")
	}
	return s, nil
}
func (r repository) DeleteAuth(_ context.Context, id string) error { delete(r, id); return nil }
func TestSessionAndCSRF(t *testing.T) {
	ctx := context.Background()
	repo := repository{}
	s := auth.New(repo, "test-credential", []string{"http://127.0.0.1"}, time.Hour)
	if _, _, err := s.Login(ctx, "test-credential", "https://foreign.test", "ip"); err == nil {
		t.Fatal("foreign origin accepted")
	}
	a, cookie, err := s.Login(ctx, "test-credential", "http://127.0.0.1", "ip")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := repo[cookie]; ok {
		t.Fatal("plaintext cookie stored")
	}
	if _, err := s.Authenticate(ctx, cookie); err != nil {
		t.Fatal(err)
	}
	if s.CheckMutation(a, "http://127.0.0.1", "") == nil {
		t.Fatal("missing csrf accepted")
	}
	if err := s.CheckMutation(a, "http://127.0.0.1", a.CSRFToken); err != nil {
		t.Fatal(err)
	}
	if err := s.Revoke(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, cookie); err == nil {
		t.Fatal("revoked cookie accepted")
	}
	for range 5 {
		s.Login(ctx, "wrong", "http://127.0.0.1", "bad")
	}
	if _, _, err := s.Login(ctx, "test-credential", "http://127.0.0.1", "bad"); err == nil {
		t.Fatal("login limit ignored")
	}
}
