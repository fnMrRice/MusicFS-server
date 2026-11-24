package storage_test

import (
	"database/sql"
	"fmt"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
)

func TestMigrationBatchesUpgradeAcrossBatchBoundaries(t *testing.T) {
	steps, err := migrations.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) == 0 {
		t.Fatal("no migrations loaded")
	}
	latest := steps[len(steps)-1].Version
	var freshSchema []string
	for _, version := range []int{0, 9, 19, 24, 27, 28, 31, 48} {
		t.Run(fmt.Sprintf("version_%03d", version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "library.sqlite")
			if version > 0 {
				db := openMigrationFixture(t, path)
				if _, err := db.ExecContext(t.Context(), "CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY)"); err != nil {
					t.Fatal(err)
				}
				for _, step := range steps {
					if step.Version > version {
						break
					}
					if _, err := db.ExecContext(t.Context(), step.SQL); err != nil {
						t.Fatalf("prepare migration %d: %v", step.Version, err)
					}
					if _, err := db.ExecContext(t.Context(), "INSERT INTO schema_migrations VALUES(?)", step.Version); err != nil {
						t.Fatal(err)
					}
				}
				seedMigrationUser(t, db)
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			}

			store, err := sqlite.Open(t.Context(), path)
			if err != nil {
				t.Fatalf("upgrade from version %d: %v", version, err)
			}
			t.Cleanup(func() { store.Close() })
			if version == 0 {
				seedMigrationUser(t, store.DB)
				freshSchema = migrationSchema(t, store.DB)
			}
			assertMigrationState(t, store.DB, latest, freshSchema)
			wantBackups := make([]string, 0, latest)
			for next := max(2, version+1); next <= latest; next++ {
				wantBackups = append(wantBackups, fmt.Sprintf("%s.before-%03d", path, next))
			}
			assertMigrationBackups(t, path, wantBackups)
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := sqlite.Open(t.Context(), path)
			if err != nil {
				t.Fatalf("reopen migrated database: %v", err)
			}
			t.Cleanup(func() { reopened.Close() })
			assertMigrationState(t, reopened.DB, latest, freshSchema)
			assertMigrationBackups(t, path, wantBackups)
		})
	}
}

func openMigrationFixture(t *testing.T, path string) *sql.DB {
	t.Helper()
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	db, err := sql.Open("sqlite", u.String()+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func seedMigrationUser(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		"INSERT INTO users VALUES('existing-user','member')",
		"INSERT INTO auth_sessions VALUES('existing-session','existing-user','original-csrf','2099-01-01T00:00:00Z')",
	} {
		if _, err := db.ExecContext(t.Context(), statement); err != nil {
			t.Fatal(err)
		}
	}
}

func assertMigrationState(t *testing.T, db *sql.DB, latest int, wantSchema []string) {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := 1
	for rows.Next() {
		var got int
		if err := rows.Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("migration history has a gap: got %d, want %d", got, want)
		}
		want++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if want-1 != latest {
		t.Fatalf("migration history ends at %d, want %d", want-1, latest)
	}
	var count int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM users u JOIN auth_sessions s ON s.user_id=u.id
		WHERE u.id='existing-user' AND u.role='member' AND s.id='existing-session'
		AND s.csrf='original-csrf' AND s.expires_at='2099-01-01T00:00:00Z'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("existing user data changed: count=%d err=%v", count, err)
	}
	if got := migrationSchema(t, db); !reflect.DeepEqual(got, wantSchema) {
		t.Fatalf("upgraded schema differs from fresh schema:\ngot: %v\nwant: %v", got, wantSchema)
	}
}

func migrationSchema(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), "SELECT sql FROM sqlite_schema WHERE sql IS NOT NULL ORDER BY type,name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var schema []string
	for rows.Next() {
		var statement string
		if err := rows.Scan(&statement); err != nil {
			t.Fatal(err)
		}
		schema = append(schema, statement)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return schema
}

func assertMigrationBackups(t *testing.T, path string, want []string) {
	t.Helper()
	got, err := filepath.Glob(path + ".before-*")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) || (len(want) > 0 && !reflect.DeepEqual(got, want)) {
		t.Fatalf("migration backups differ: got %v, want %v", got, want)
	}
}
