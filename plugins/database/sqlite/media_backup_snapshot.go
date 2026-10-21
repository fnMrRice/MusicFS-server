package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/storage/migrations"
)

func backupSchemaVersion() (int, error) {
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		return 0, err
	}
	version := 0
	for _, entry := range entries {
		v, err := strconv.Atoi(strings.Split(entry.Name(), "_")[0])
		if err == nil && v > version {
			version = v
		}
	}
	return version, nil
}

// The snapshot is read with immutable=1 after VACUUM INTO has closed it. It is
// never opened through Store.Open, which could migrate or otherwise alter it.
func readBackupSnapshot(ctx context.Context, path string) ([]mediastore.BackupObject, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, domain.Fail("ACCESS_DENIED", "Backup database must be a regular file")
	}
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	db, err := sql.Open("sqlite", u.String()+"?mode=ro&immutable=1")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if err = validateBackupDatabase(ctx, db); err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(ctx, `SELECT p.document,p.id,p.owner,p.object_id,p.status FROM media_publications p
 WHERE EXISTS(SELECT 1 FROM media_content_objects c WHERE c.publication_id=p.id)
 OR EXISTS(SELECT 1 FROM fingerprint_indexes i WHERE i.publication_id=p.id)
 OR EXISTS(SELECT 1 FROM media_derived_objects d WHERE d.publication_id=p.id) ORDER BY p.object_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	objects := []mediastore.BackupObject{}
	for rows.Next() {
		if len(objects) >= mediastore.MaxBackupObjects {
			return nil, domain.Fail("RATE_LIMITED", "Backup reference count exceeds the supported limit")
		}
		var raw []byte
		var id, owner, objectID, status string
		var p mediastore.Publication
		if err = rows.Scan(&raw, &id, &owner, &objectID, &status); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		p.Object.RelativePath = "objects/" + p.Object.ID
		if p.ID != id || p.Owner != owner || owner == "" || p.Object.ID != objectID || p.Status != status || (status != "referenced" && status != "finished" && status != "unavailable") {
			return nil, domain.Fail("CONFLICT", "Backup reference has inconsistent publication metadata")
		}
		if err = p.Object.Validate(); err != nil {
			return nil, err
		}
		objects = append(objects, mediastore.BackupObject{Owner: owner, PublicationID: id, Object: p.Object, State: "pending"})
	}
	return objects, rows.Err()
}

func validateBackupDatabase(ctx context.Context, db *sql.DB) error {
	want, err := backupSchemaVersion()
	if err != nil {
		return err
	}
	var count, first, last int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*),COALESCE(MIN(version),0),COALESCE(MAX(version),0) FROM schema_migrations").Scan(&count, &first, &last); err != nil {
		return err
	}
	if first != 1 || last != want || count != want {
		return domain.Fail("CONFLICT", "Backup database schema does not match this server")
	}
	var integrity string
	if err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return err
	}
	if integrity != "ok" {
		return domain.Fail("RESOURCE_CHANGED", "Backup database integrity check failed")
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	broken := rows.Next()
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if broken {
		return domain.Fail("RESOURCE_CHANGED", "Backup database references are incomplete")
	}
	var invalid int
	err = db.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM media_content_objects c JOIN media_publications p ON p.id=c.publication_id WHERE c.owner!=p.owner OR c.id!=p.object_id)
 +(SELECT COUNT(*) FROM fingerprint_indexes i JOIN media_publications p ON p.id=i.publication_id WHERE i.owner!=p.owner OR i.object_id!=p.object_id)
 +(SELECT COUNT(*) FROM media_derived_objects d JOIN media_publications p ON p.id=d.publication_id WHERE d.owner!=p.owner OR d.id!=p.object_id)`).Scan(&invalid)
	if err != nil {
		return err
	}
	if invalid != 0 {
		return domain.Fail("RESOURCE_CHANGED", "Backup media references are inconsistent")
	}
	return nil
}
