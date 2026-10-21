package sqlite

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

// OpenMaintenance never creates or migrates a database. The caller must hold
// the exclusive server.lock for its lifetime, or own a private restore directory.
func OpenMaintenance(ctx context.Context, path string) (*Store, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, domain.Fail("INVALID_ARGUMENT", "An existing regular database is required")
	}
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p}
	db, err := sql.Open("sqlite", u.String()+"?mode=rw&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err = validateBackupDatabase(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{DB: db}, nil
}

func BackupSchemaVersion() (int, error) { return backupSchemaVersion() }

func InspectBackup(ctx context.Context, path string) ([]mediastore.BackupObject, error) {
	return readBackupSnapshot(ctx, path)
}

// PrepareRestoredDatabase preserves data and task journals, but no restored
// browser cookie or old configuration intent can authorize a new installation.
func (s *Store) PrepareRestoredDatabase(ctx context.Context, objects []mediastore.BackupObject) error {
	for _, object := range objects {
		if object.State == "missing" {
			if err := s.InvalidatePublication(ctx, object.Owner, object.PublicationID, "NOT_FOUND"); err != nil {
				return err
			}
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range []string{
		"DELETE FROM idempotency WHERE owner IN (SELECT id FROM auth_sessions)",
		"DELETE FROM auth_sessions",
		"DELETE FROM processed_cache",
		"UPDATE playback_sessions SET state='expired' WHERE state='active'",
		"UPDATE config_operations SET status='superseded' WHERE status IN ('prepared','saved')",
	} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}
