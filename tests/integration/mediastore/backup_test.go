package mediastore_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/transform/thumbnail"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func TestBackupCopiesSnapshotReferencesAndRestoresMissingState(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	old := upload(t, f, "historical unselected content")
	current := upload(t, f, "selected missing content")
	preview := upload(t, f, "unselected preview")
	if _, err := f.service.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: f.metadata.Target, ObjectID: current.ID}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.root, "objects", current.ID)); err != nil {
		t.Fatal(err)
	}
	result, err := mediastore.NewBackups(f.store, f.files).Create(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if result.Backup.State != "finished" || result.Backup.ObjectCount != 3 || result.Manifest.MissingCount != 1 {
		t.Fatalf("incomplete backup: %+v", result)
	}
	raw, err := os.ReadFile(filepath.Join(result.Directory, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest mediastore.BackupManifest
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	dbBytes, err := os.ReadFile(filepath.Join(result.Directory, manifest.Database.Name))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(dbBytes)
	if int64(len(dbBytes)) != manifest.Database.Size || hex.EncodeToString(digest[:]) != manifest.Database.SHA256 {
		t.Fatal("snapshot checksum differs")
	}
	states := map[string]string{}
	for _, obj := range manifest.Objects {
		states[obj.Object.ID] = obj.State
	}
	if states[old.ID] != "copied" || states[preview.ID] != "copied" || states[current.ID] != "missing" {
		t.Fatalf("references lost: %+v", states)
	}
	// A fresh data directory is populated only from the backup, never the live DB.
	restoredPath := filepath.Join(t.TempDir(), "db.sqlite")
	if err = os.WriteFile(restoredPath, dbBytes, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := sqlite.Open(ctx, restoredPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	files, err := mediastore.OpenFiles(filepath.Join(result.Directory, "media"))
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	service := mediastore.NewContent(s, s, files, thumbnail.New())
	for _, obj := range []mediastore.ContentObject{old, preview} {
		_, in, err := service.Open(ctx, "admin", obj.ID)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(in)
		in.Close()
		if err != nil || int64(len(data)) != obj.Size {
			t.Fatal("restored bytes differ", err)
		}
	}
	if _, _, err = service.Open(ctx, "admin", current.ID); err == nil {
		t.Fatal("missing restore appeared available")
	}
	selection, err := s.ContentSelection(ctx, "admin", f.metadata.Target.SourceTrackID)
	if err != nil || len(selection.Bindings) != 1 || selection.Bindings[0].State != "unavailable" || !selection.Bindings[0].ManualLocked {
		t.Fatalf("missing manual content was discarded: %+v %v", selection, err)
	}
	if _, _, err = service.Open(ctx, "other", old.ID); err == nil {
		t.Fatal("backup restore lost ownership")
	}
}

type interruptedBackup struct {
	mediastore.BackupRepository
	cancel context.CancelFunc
}

func (r interruptedBackup) BackupObjects(ctx context.Context, id, after string, limit int) ([]mediastore.BackupObject, error) {
	objects, err := r.BackupRepository.BackupObjects(ctx, id, after, limit)
	r.cancel()
	return objects, err
}

func TestInterruptedBackupHasNoCompletenessMarkerAndNewRunSucceeds(t *testing.T) {
	f := setup(t)
	upload(t, f, "private manual content")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	parent := t.TempDir()
	partial, err := mediastore.NewBackups(interruptedBackup{f.store, cancel}, f.files).Create(ctx, parent)
	if !errors.Is(err, context.Canceled) || partial.Directory == "" {
		t.Fatalf("interruption lost: %+v %v", partial, err)
	}
	if _, err = os.Stat(filepath.Join(partial.Directory, "manifest.json")); !os.IsNotExist(err) {
		t.Fatal("partial backup appears complete")
	}
	complete, err := mediastore.NewBackups(f.store, f.files).Create(context.Background(), parent)
	if err != nil || complete.Directory == partial.Directory || len(complete.Manifest.Objects) != 1 {
		t.Fatal("fresh backup could not recover", err)
	}
	if _, err = os.Stat(filepath.Join(partial.Directory, "db.sqlite")); err != nil {
		t.Fatal("prior incomplete evidence was overwritten")
	}
}
