package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func readMediaBackup(row rowScanner) (mediastore.Backup, string, error) {
	var b mediastore.Backup
	var path, created, updated string
	err := row.Scan(&b.ID, &path, &b.State, &b.ObjectCount, &b.MissingCount, &created, &updated)
	if err != nil {
		return b, path, err
	}
	if b.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return b, path, err
	}
	b.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	return b, path, err
}

const backupColumns = "id,database_path,state,object_count,missing_count,created_at,updated_at"

func (s *Store) CreateMediaBackup(ctx context.Context, id, path string) (mediastore.Backup, error) {
	var empty mediastore.Backup
	if !mediaID(id) || !filepath.IsAbs(path) || filepath.Ext(path) != ".sqlite" {
		return empty, domain.Fail("INVALID_ARGUMENT", "Invalid backup snapshot target")
	}
	path = filepath.Clean(path)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	prior, savedPath, err := readMediaBackup(tx.QueryRowContext(ctx, "SELECT "+backupColumns+" FROM media_backups WHERE id=?", id))
	if err == nil {
		tx.Rollback()
		if savedPath != path {
			return empty, domain.Fail("CONFLICT", "Backup identity has a different destination")
		}
		if prior.State == "preparing" {
			return empty, domain.Fail("CONFLICT", "Interrupted backup snapshot requires a new identity and destination")
		}
		return prior, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		tx.Rollback()
		return empty, err
	}
	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM media_backups WHERE database_path=?", path).Scan(&exists); err != nil {
		tx.Rollback()
		return empty, err
	}
	if exists != 0 {
		tx.Rollback()
		return empty, domain.Fail("CONFLICT", "Backup destination is already reserved")
	}
	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, "INSERT INTO media_backups(id,database_path,state,created_at,updated_at) VALUES(?,?,'preparing',?,?)", id, path, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		tx.Rollback()
		return empty, err
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	// VACUUM INTO neither overwrites an existing file nor reads references from a
	// later live transaction. An interrupted intent remains explicitly preparing.
	if _, err = os.Lstat(path); err == nil {
		return empty, domain.Fail("CONFLICT", "Backup destination already exists")
	} else if !os.IsNotExist(err) {
		return empty, err
	}
	if err = s.Backup(ctx, path); err != nil {
		return empty, err
	}
	objects, err := readBackupSnapshot(ctx, path)
	if err != nil {
		return empty, err
	}
	tx, err = s.DB.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	for _, object := range objects {
		var deleted int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM media_garbage WHERE object_id=? AND state IN ('claimed','deleted')", object.Object.ID).Scan(&deleted); err != nil {
			return empty, err
		}
		if deleted != 0 {
			return empty, domain.Fail("CONFLICT", "Backup reference conflicts with garbage collection")
		}
		raw, err := json.Marshal(object)
		if err != nil {
			return empty, err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO media_backup_objects(backup_id,object_id,owner,publication_id,state,document) VALUES(?,?,?,?,'pending',?)", id, object.Object.ID, object.Owner, object.PublicationID, raw)
		if err != nil {
			return empty, err
		}
	}
	_, err = tx.ExecContext(ctx, "UPDATE media_backups SET state='prepared',object_count=?,updated_at=? WHERE id=? AND state='preparing'", len(objects), time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return empty, err
	}
	result, _, err := readMediaBackup(tx.QueryRowContext(ctx, "SELECT "+backupColumns+" FROM media_backups WHERE id=?", id))
	if err != nil {
		return empty, err
	}
	return result, tx.Commit()
}

func (s *Store) BackupObjects(ctx context.Context, id, after string, limit int) ([]mediastore.BackupObject, error) {
	if !mediaID(id) || limit < 1 || limit > 128 || after != "" && !mediaID(after) {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid backup page")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var state string
	if err = tx.QueryRowContext(ctx, "SELECT state FROM media_backups WHERE id=?", id).Scan(&state); errors.Is(err, sql.ErrNoRows) {
		return nil, domain.Fail("NOT_FOUND", "Backup not found")
	} else if err != nil {
		return nil, err
	}
	if state == "preparing" {
		return nil, domain.Fail("CONFLICT", "Backup snapshot is incomplete")
	}
	rows, err := tx.QueryContext(ctx, "SELECT document,state FROM media_backup_objects WHERE backup_id=? AND object_id>? ORDER BY object_id LIMIT ?", id, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []mediastore.BackupObject{}
	for rows.Next() {
		var raw []byte
		var state string
		var object mediastore.BackupObject
		if err = rows.Scan(&raw, &state); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &object); err != nil {
			return nil, err
		}
		object.State = state
		object.Object.RelativePath = "objects/" + object.Object.ID
		if err = object.Object.Validate(); err != nil {
			return nil, err
		}
		result = append(result, object)
	}
	return result, rows.Err()
}

func (s *Store) RecordBackupObject(ctx context.Context, id, objectID, state string) error {
	if !mediaID(id) || !mediaID(objectID) || (state != "copied" && state != "missing") {
		return domain.Fail("INVALID_ARGUMENT", "Invalid backup object receipt")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var current, backupState string
	err = tx.QueryRowContext(ctx, "SELECT o.state,b.state FROM media_backup_objects o JOIN media_backups b ON b.id=o.backup_id WHERE o.backup_id=? AND o.object_id=?", id, objectID).Scan(&current, &backupState)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Fail("NOT_FOUND", "Backup object not found")
	}
	if err != nil {
		return err
	}
	if current == state {
		return nil
	}
	if current != "pending" || backupState != "prepared" {
		return domain.Fail("CONFLICT", "Backup receipt is already final")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE media_backup_objects SET state=? WHERE backup_id=? AND object_id=?", state, id, objectID); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE media_backups SET missing_count=(SELECT COUNT(*) FROM media_backup_objects WHERE backup_id=? AND state='missing'),updated_at=? WHERE id=?", id, time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) FinishMediaBackup(ctx context.Context, id string) error {
	if !mediaID(id) {
		return domain.Fail("INVALID_ARGUMENT", "Invalid backup identity")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	backup, _, err := readMediaBackup(tx.QueryRowContext(ctx, "SELECT "+backupColumns+" FROM media_backups WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Fail("NOT_FOUND", "Backup not found")
	}
	if err != nil {
		return err
	}
	if backup.State == "finished" {
		return nil
	}
	var pending int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM media_backup_objects WHERE backup_id=? AND state='pending'", id).Scan(&pending); err != nil {
		return err
	}
	if backup.State != "prepared" || pending != 0 {
		return domain.Fail("CONFLICT", "Backup snapshot or object receipts are incomplete")
	}
	_, err = tx.ExecContext(ctx, "UPDATE media_backups SET state='finished',updated_at=? WHERE id=?", time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

var _ mediastore.BackupRepository = (*Store)(nil)
