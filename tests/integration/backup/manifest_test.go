package backup_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/backup"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
)

func TestBackupRejectsChecksumValidOmittedMediaReference(t *testing.T) {
	doc, _, _ := installation(t)
	ctx := context.Background()
	b, err := backup.Create(ctx, doc, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(b.Directory, "manifest.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var media mediastore.BackupManifest
	if err = json.Unmarshal(raw, &media); err != nil {
		t.Fatal(err)
	}
	if len(media.Objects) == 0 {
		t.Fatal("fixture has no referenced media")
	}
	media.Objects = media.Objects[:0]
	raw, err = json.Marshal(media)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	for i := range b.Manifest.Files {
		if b.Manifest.Files[i].Path == "manifest.json" {
			b.Manifest.Files[i].Size = int64(len(raw))
			b.Manifest.Files[i].SHA256 = hex.EncodeToString(hash[:])
		}
	}
	raw, err = json.Marshal(b.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(b.Directory, "bundle.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = backup.Verify(ctx, b.Directory); err == nil {
		t.Fatal("checksum-valid omitted database reference accepted")
	}
	destination := filepath.Join(t.TempDir(), "restored")
	if _, err = backup.Restore(ctx, b.Directory, destination); err == nil {
		t.Fatal("omitted database reference restored")
	}
	if _, err = os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatal("invalid bundle created restore directory")
	}
}

func TestBackupRejectsLinkedProviderState(t *testing.T) {
	doc, _, _ := installation(t)
	outside := filepath.Join(t.TempDir(), "private.json")
	if err := os.WriteFile(outside, []byte("private fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(doc.Config.DataRoot, "providers", "local", "linked.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("Symlink creation unavailable: %v", err)
	}
	if _, err := backup.Create(context.Background(), doc, t.TempDir()); err == nil {
		t.Fatal("provider state link accepted")
	}
}
