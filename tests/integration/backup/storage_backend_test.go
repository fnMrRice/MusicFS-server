package backup_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/backup"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

func TestRealStorageWorkerBackupRestoresWithoutItsExecutable(t *testing.T) {
	doc, trackID, _ := installation(t)
	executable := filepath.Join(t.TempDir(), "storage-worker")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	command := exec.Command("go", "build", "-o", executable, "./plugins/database/sqlite/cmd/musicfs-plugin-storage-sqlite")
	command.Dir = filepath.Join("..", "..", "..")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build storage worker: %v %s", err, output)
	}
	raw, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	doc.Config.Storage = config.StorageConfig{Backend: "plugin", Executable: executable, SHA256: hex.EncodeToString(digest[:])}
	ctx := context.Background()
	bundle, err := backup.Create(ctx, doc, t.TempDir())
	if err != nil || bundle.Manifest.StorageBackend != "plugin" {
		t.Fatalf("external maintenance backup: %+v %v", bundle.Manifest, err)
	}
	if err = os.Remove(executable); err != nil {
		t.Fatal(err)
	}
	if _, err = backup.Verify(ctx, bundle.Directory); err != nil {
		t.Fatalf("verification depended on backed-up executable: %v", err)
	}
	result, err := backup.Restore(ctx, bundle.Directory, filepath.Join(t.TempDir(), "restored"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(result.Configuration, map[string]string{"ADMIN_TOKEN": domain.NewID()})
	if err != nil || cfg.Storage.Backend != "sqlite" || len(cfg.AudioDSP.Plugins) != 0 || len(cfg.AudioDSP.Chain) != 0 {
		t.Fatalf("unsafe restored runtime configuration: %+v %v", cfg.Storage, err)
	}
	store, err := storage.Open(ctx, "sqlite", storage.Options{DataRoot: cfg.DataRoot})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.FindTrack(ctx, "admin", trackID); err != nil {
		t.Fatalf("external backup lost library: %v", err)
	}
}

func TestBackupUsesSelectedBackendAndRestoresWithoutExecutingIt(t *testing.T) {
	doc, trackID, _ := installation(t)
	ctx := context.Background()
	driver := sqlite.Driver()
	driver.Name = "portable-" + domain.NewID()
	driver.SnapshotName = "portable.sqlite"
	openMaintenance := driver.OpenMaintenance
	maintenanceCalls := 0
	runtimeCalls := 0
	driver.OpenMaintenance = func(ctx context.Context, options storage.Options) (storage.Maintenance, error) {
		maintenanceCalls++
		return openMaintenance(ctx, options)
	}
	driver.Open = func(context.Context, storage.Options) (storage.Backend, error) {
		runtimeCalls++
		return nil, domain.Fail("UNSUPPORTED", "Runtime must not execute during backup or restore")
	}
	if err := storage.Register(driver); err != nil {
		t.Fatal(err)
	}
	doc.Config.Storage = config.StorageConfig{Backend: driver.Name}
	bundle, err := backup.Create(ctx, doc, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if bundle.Manifest.StorageBackend != driver.Name || maintenanceCalls != 1 {
		t.Fatalf("wrong backup backend: %s, calls: %d", bundle.Manifest.StorageBackend, maintenanceCalls)
	}
	if _, err = backup.Verify(ctx, bundle.Directory); err != nil {
		t.Fatal(err)
	}
	restored, err := backup.Restore(ctx, bundle.Directory, filepath.Join(t.TempDir(), "restored"))
	if err != nil {
		t.Fatal(err)
	}
	if maintenanceCalls != 1 || runtimeCalls != 0 {
		t.Fatalf("backup verification or restore invoked custom backend: maintenance=%d, runtime=%d", maintenanceCalls, runtimeCalls)
	}
	loaded, err := config.Load(restored.Configuration, map[string]string{"ADMIN_TOKEN": domain.NewID()})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Storage.Backend != "sqlite" || loaded.Storage.Executable != "" || len(loaded.Storage.Options) != 0 {
		t.Fatalf("restore retained external storage authority: %+v", loaded.Storage)
	}
	store, err := storage.Open(ctx, loaded.Storage.Backend, storage.Options{DataRoot: loaded.DataRoot})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.FindTrack(ctx, "admin", trackID); err != nil {
		t.Fatalf("portable restore lost library: %v", err)
	}
}

func TestLegacyBackupDefaultsToSQLiteAndUnknownBackendFailsClosed(t *testing.T) {
	doc, _, _ := installation(t)
	ctx := context.Background()
	bundle, err := backup.Create(ctx, doc, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeManifest := func() {
		raw, err := json.Marshal(bundle.Manifest)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(bundle.Directory, "bundle.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	bundle.Manifest.StorageBackend = ""
	writeManifest()
	if _, err = backup.Verify(ctx, bundle.Directory); err != nil {
		t.Fatalf("legacy backup without backend identity rejected: %v", err)
	}
	bundle.Manifest.StorageBackend = "missing-storage-backend"
	writeManifest()
	if _, err = backup.Verify(ctx, bundle.Directory); err == nil {
		t.Fatal("unknown snapshot backend accepted")
	}
	destination := filepath.Join(t.TempDir(), "untouched")
	if _, err = backup.Restore(ctx, bundle.Directory, destination); err == nil {
		t.Fatal("unknown snapshot backend restored")
	}
	if _, err = os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("unknown snapshot backend created restore directory: %v", err)
	}
}
