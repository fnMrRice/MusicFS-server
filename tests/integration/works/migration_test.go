package works_test

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
)

func TestWorkMigrationUpgradesExistingDatabaseAndPreservesBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.sqlite")
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := url.URL{Scheme: "file", Path: uriPath}
	db, err := sql.Open("sqlite", uri.String()+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.ExecContext(t.Context(), "CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	steps, err := migrations.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range steps {
		if step.Version > 25 {
			break
		}
		if _, err = db.ExecContext(t.Context(), step.SQL); err != nil {
			t.Fatalf("prepare migration %d: %v", step.Version, err)
		}
		if _, err = db.ExecContext(t.Context(), "INSERT INTO schema_migrations VALUES(?)", step.Version); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.ExecContext(t.Context(), "INSERT INTO users VALUES('existing-user','member')"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	var role string
	if err = store.DB.QueryRowContext(t.Context(), "SELECT role FROM users WHERE id='existing-user'").Scan(&role); err != nil || role != "member" {
		t.Fatal("migration changed existing data", err)
	}
	var version int
	if err = store.DB.QueryRowContext(t.Context(), "SELECT MAX(version) FROM schema_migrations").Scan(&version); err != nil || version != steps[len(steps)-1].Version {
		t.Fatal("database did not reach the latest schema", version, err)
	}
	if _, err = os.Stat(path + ".before-026"); err != nil {
		t.Fatal("migration backup is missing", err)
	}
	created := createWork(t, store, "existing-user", sampleRequest())
	actual, err := store.Work(t.Context(), "existing-user", created.ID)
	if err != nil || actual.Title != created.Title {
		t.Fatal("upgraded database cannot store works", err)
	}
}
