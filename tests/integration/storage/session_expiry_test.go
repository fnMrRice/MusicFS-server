package storage_test

import (
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
)

func checkSessionRenewal(t *testing.T, repo auth.Repository) {
	t.Helper()
	ctx := t.Context()
	service := auth.New(repo, "unused", nil, time.Hour)
	initial := auth.Session{ID: "sliding-session", User: auth.User{ID: "admin", Role: "admin"}, ExpiresAt: time.Now().UTC().Add(time.Minute)}
	if err := repo.SaveAuth(ctx, initial); err != nil {
		t.Fatal(err)
	}
	renewed, err := service.Renew(ctx, initial)
	if err != nil || !renewed.ExpiresAt.After(initial.ExpiresAt) {
		t.Fatal("session deadline did not advance", err)
	}
	if expiry, err := repo.UpdateAuthExpiry(ctx, initial.ID, initial.ExpiresAt.Add(time.Minute)); err != nil || !expiry.Equal(renewed.ExpiresAt) {
		t.Fatal("stale write overwrote a later deadline", err)
	}
	captured := initial
	captured.ExpiresAt = time.Now().Add(-time.Second)
	again, err := service.Renew(ctx, captured)
	if err != nil || again.ExpiresAt.Before(renewed.ExpiresAt) {
		t.Fatal("concurrent renewal shortened the session", err)
	}
	if err := service.Revoke(ctx, initial.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Renew(ctx, renewed); err == nil {
		t.Fatal("stale renewal restored a revoked session")
	}
	initial.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	if err := repo.SaveAuth(ctx, initial); err != nil {
		t.Fatal(err)
	}
	if expiry, err := repo.UpdateAuthExpiry(ctx, initial.ID, renewed.ExpiresAt); err != nil || !expiry.IsZero() {
		t.Fatal("stale renewal revived an expired session", err)
	}
}
