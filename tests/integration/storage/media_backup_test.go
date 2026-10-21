package storage_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func backupSnapshotObjects(t *testing.T, path string) map[string]bool {
	t.Helper()
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p}
	db, err := sql.Open("sqlite", u.String()+"?mode=ro&immutable=1")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query("SELECT id FROM media_content_objects UNION SELECT object_id FROM fingerprint_indexes")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	ids := map[string]bool{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids[id] = true
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestMediaBackupUsesExactSnapshotAndPinsHistoricalObjects(t *testing.T) {
	s, _, tracks := matchingFixture(t, 2)
	ctx := context.Background()
	old := contentPublish(t, s, "admin", contentInput(tracks[0], "zh", "historical manual lyrics"))
	contentSelect(t, s, old, 0, false)
	next := contentPublish(t, s, "admin", contentInput(tracks[0], "zh", "current manual lyrics"))
	contentSelect(t, s, next, 1, false)
	preview := contentPublish(t, s, "admin", contentInput(tracks[1], "en", "unselected preview"))
	if err := s.InvalidateContent(ctx, "admin", preview.ID); err != nil {
		t.Fatal(err)
	}
	index := matchingFingerprint(t, s, fpTarget(tracks[1]))
	if err := s.InvalidateFingerprint(ctx, "admin", index.ID); err != nil {
		t.Fatal(err)
	}
	private := matchingPrivateTrack(t, s)
	privateObject := contentPublish(t, s, "bob", contentInput(private, "und", "private owner's backup content"))
	id, path := domain.NewID(), filepath.Join(t.TempDir(), "db.sqlite")
	backup, err := s.CreateMediaBackup(ctx, id, path)
	if err != nil || backup.State != "prepared" || backup.ObjectCount != 5 {
		t.Fatalf("backup failed: %+v %v", backup, err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	wanted := backupSnapshotObjects(t, path)
	// This live publication must not leak into the already captured manifest.
	late := contentPublish(t, s, "admin", contentInput(tracks[1], "ja", "late upload"))
	all := []mediastore.BackupObject{}
	after := ""
	for {
		page, err := s.BackupObjects(ctx, id, after, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		all = append(all, page...)
		after = page[len(page)-1].Object.ID
	}
	actual := map[string]bool{}
	owners := map[string]bool{}
	for _, object := range all {
		actual[object.Object.ID] = true
		owners[object.Owner] = true
		if object.State != "pending" || object.Object.RelativePath != "objects/"+object.Object.ID {
			t.Fatal("invalid backup descriptor")
		}
	}
	if !reflect.DeepEqual(actual, wanted) || actual[late.ID] || !actual[old.ID] || !actual[preview.ID] || !actual[index.Object.ID] || !actual[privateObject.ID] || !owners["bob"] {
		t.Fatal("snapshot references were taken from live state or omitted history")
	}
	err = s.FinishMediaBackup(ctx, id)
	matchingError(t, err, "CONFLICT")
	for i, object := range all {
		state := "copied"
		if i == 0 {
			state = "missing"
		}
		if err = s.RecordBackupObject(ctx, id, object.Object.ID, state); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.FinishMediaBackup(ctx, id); err != nil {
		t.Fatal(err)
	}
	finished, err := s.CreateMediaBackup(ctx, id, path)
	if err != nil || finished.State != "finished" || finished.MissingCount != 1 || finished.ObjectCount != 5 {
		t.Fatal("backup receipts did not persist", err)
	}
	if err = s.FinishMediaBackup(ctx, id); err != nil {
		t.Fatal(err)
	}
	replayed, err := s.CreateMediaBackup(ctx, id, path)
	if err != nil || !reflect.DeepEqual(finished, replayed) {
		t.Fatal("backup retry changed completed manifest", err)
	}
	if err = s.RecordBackupObject(ctx, id, all[0].Object.ID, "copied"); err == nil {
		t.Fatal("final missing receipt was rewritten")
	}
	afterBytes, err := os.ReadFile(path)
	if err != nil || sha256.Sum256(before) != sha256.Sum256(afterBytes) {
		t.Fatal("reading backup metadata mutated the database snapshot", err)
	}
	var pins int
	s.DB.QueryRow("SELECT COUNT(*) FROM media_backup_objects WHERE backup_id=?", id).Scan(&pins)
	if pins != 5 {
		t.Fatal("backup references were not pinned")
	}
}

func TestMediaBackupInterruptedSnapshotNeverOverwritesAndRequiresNewIdentity(t *testing.T) {
	s, _, _ := matchingFixture(t, 1)
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "db.sqlite")
	original := []byte("existing backup must not be overwritten")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	id := domain.NewID()
	if _, err := s.CreateMediaBackup(ctx, id, path); err == nil {
		t.Fatal("existing snapshot overwritten")
	}
	got, err := os.ReadFile(path)
	if err != nil || !reflect.DeepEqual(got, original) {
		t.Fatal("existing bytes changed", err)
	}
	var state string
	s.DB.QueryRow("SELECT state FROM media_backups WHERE id=?", id).Scan(&state)
	if state != "preparing" {
		t.Fatal("failed snapshot intent was lost")
	}
	_, err = s.CreateMediaBackup(ctx, id, path)
	matchingError(t, err, "CONFLICT")
	_, err = s.CreateMediaBackup(ctx, domain.NewID(), path)
	matchingError(t, err, "CONFLICT")
	_, err = s.BackupObjects(ctx, id, "", 128)
	matchingError(t, err, "CONFLICT")
	_, err = s.CreateMediaBackup(ctx, id, filepath.Join(t.TempDir(), "db.sqlite"))
	matchingError(t, err, "CONFLICT")
	if _, err = s.CreateMediaBackup(ctx, domain.NewID(), filepath.Join(t.TempDir(), "db.sqlite")); err != nil {
		t.Fatal("new snapshot could not proceed", err)
	}
}

func TestMediaBackupRejectsInconsistentSchemaAndReferences(t *testing.T) {
	t.Run("schema", func(t *testing.T) {
		s, _, _ := matchingFixture(t, 1)
		if _, err := s.DB.Exec("DELETE FROM schema_migrations WHERE version=(SELECT MAX(version) FROM schema_migrations)"); err != nil {
			t.Fatal(err)
		}
		_, err := s.CreateMediaBackup(context.Background(), domain.NewID(), filepath.Join(t.TempDir(), "db.sqlite"))
		matchingError(t, err, "CONFLICT")
	})
	t.Run("references", func(t *testing.T) {
		s, _, tracks := matchingFixture(t, 1)
		object := contentPublish(t, s, "admin", contentInput(tracks[0], "und", "bound content"))
		if _, err := s.DB.Exec("UPDATE media_publications SET object_id=? WHERE object_id=?", domain.NewID(), object.ID); err != nil {
			t.Fatal(err)
		}
		_, err := s.CreateMediaBackup(context.Background(), domain.NewID(), filepath.Join(t.TempDir(), "db.sqlite"))
		matchingError(t, err, "RESOURCE_CHANGED")
	})
}
