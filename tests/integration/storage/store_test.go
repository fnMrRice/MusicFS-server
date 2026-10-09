package storage_test

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
	"path/filepath"
	"testing"
	"time"
)

func TestMigrationPersistenceBackupAndCancellation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "library.sqlite")
	s, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	a := auth.Session{ID: "digest", User: auth.User{ID: "admin", Role: "admin"}, CSRFToken: "csrf", ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if err = s.SaveAuth(ctx, a); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(t.TempDir(), "backup.sqlite")
	if err = s.Backup(ctx, backup); err != nil {
		t.Fatal(err)
	}
	s.Close()
	for _, p := range []string{path, backup} {
		s, err = sqlite.Open(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		got, err := s.FindAuth(ctx, a.ID)
		if err != nil || got.User.ID != "admin" {
			t.Fatal("auth state did not persist", err)
		}
		s.Close()
	}
	s, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = s.DB.ExecContext(cancelled, "INSERT INTO users VALUES('other','admin')"); err == nil {
		t.Fatal("cancelled write succeeded")
	}
}

func TestNewerDatabaseIsRejected(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "newer.sqlite")
	s, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ExecContext(ctx, "INSERT INTO schema_migrations VALUES(999)"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	if s, err = sqlite.Open(ctx, path); err == nil {
		s.Close()
		t.Fatal("newer schema accepted")
	}
}
