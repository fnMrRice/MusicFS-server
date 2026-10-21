package backup_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/storage/backup"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"go.yaml.in/yaml/v3"
)

func installation(t *testing.T) (*config.Document, string, string) {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	raw := []byte("configVersion: 1\nallowLoopbackHttp: true\nallowedOrigins: [http://127.0.0.1:8080]\nplugins: []\n")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	doc, err := config.ReadDocument(path, map[string]string{"ADMIN_TOKEN": "test-credential-must-never-be-backed-up"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, filepath.Join(doc.Config.DataRoot, "musicfs.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	_, _, err = store.Enqueue(ctx, "admin", "local", testkit.ScanPlan("local"), "backup-fixture-scan")
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = store.Publish(ctx, job, testkit.ScanPublication(job, []library.Observation{{Observation: pluginproto.Observation{ExternalID: "one", RootID: "local", RelativePath: "track.wav", ObservedVersion: "v1", DisplayName: "Synthetic track", MIME: "audio/wav", SizeBytes: 100}}}))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Finish(ctx, job, 1, nil); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListTracks(ctx, "admin", "local", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	track := page.Items[0]
	files, err := mediastore.OpenFiles(doc.Config.MediaRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	content := mediastore.NewContent(store, store, files)
	target := mediastore.ContentTarget{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version}
	object, err := content.Upload(ctx, "admin", mediastore.ContentMetadata{Target: target, Field: "lyrics", Language: "en", Format: "text", Origin: mediastore.Origin{Kind: "edit"}}, "text/plain", domain.NewID(), strings.NewReader("Keep my manually edited lyrics\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = content.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: target, ObjectID: object.ID}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	if err = store.SaveAuth(ctx, auth.Session{ID: "old-browser-cookie", User: auth.User{ID: "admin", Role: "admin"}, ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(doc.Config.DataRoot, "providers", "local", "identity.json")
	if err = os.MkdirAll(filepath.Dir(state), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(state, []byte("{\"stable\":\"identity\"}"), 0600); err != nil {
		t.Fatal(err)
	}
	return doc, track.ID, object.ID
}

func TestOfflineBackupRestorePreservesManualFilesAndRevokesCookies(t *testing.T) {
	doc, trackID, objectID := installation(t)
	ctx := context.Background()
	result, err := backup.Create(ctx, doc, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest.MissingMedia != 0 {
		t.Fatal("unexpected missing media")
	}
	if _, err = backup.Verify(ctx, result.Directory); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{"configuration.yaml", "configuration.source.yaml", "bundle.json"} {
		raw, err := os.ReadFile(filepath.Join(result.Directory, file))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte(doc.Config.AdminToken)) {
			t.Fatal("administrator credential included")
		}
	}
	destination := filepath.Join(t.TempDir(), "restored")
	restored, err := backup.Restore(ctx, result.Directory, destination)
	if err != nil {
		t.Fatal(err)
	}
	if !restored.RequiresLogin || restored.WriteBackEnabled || restored.PluginsEnabled {
		t.Fatal("restore enabled credentials or source mutation")
	}
	loaded, err := config.Load(restored.Configuration, map[string]string{"ADMIN_TOKEN": domain.NewID()})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DataRoot != filepath.Join(destination, "data") || loaded.Hashing.Auto || loaded.WriteBack.Enabled || loaded.Metadata.Automatic.Enabled {
		t.Fatal("unsafe restored defaults")
	}
	store, err := sqlite.OpenMaintenance(ctx, filepath.Join(loaded.DataRoot, "musicfs.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.FindAuth(ctx, "old-browser-cookie"); err == nil {
		t.Fatal("restored browser credential remained valid")
	}
	files, err := mediastore.OpenFiles(loaded.MediaRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer files.Close()
	content := mediastore.NewContent(store, store, files)
	selection, err := content.ContentSelection(ctx, "admin", trackID)
	if err != nil || len(selection.Bindings) != 1 || !selection.Bindings[0].ManualLocked {
		t.Fatalf("selection lost: %+v %v", selection, err)
	}
	_, stream, err := content.Open(ctx, "admin", objectID)
	if err != nil {
		t.Fatal(err)
	}
	stream.Close()
	state, err := os.ReadFile(filepath.Join(loaded.DataRoot, "providers", "local", "identity.json"))
	if err != nil || string(state) != "{\"stable\":\"identity\"}" {
		t.Fatal("provider state lost", err)
	}
	if _, err = backup.Restore(ctx, result.Directory, destination); err == nil {
		t.Fatal("existing installation overwritten")
	}
	if _, err = backup.Restore(ctx, result.Directory, filepath.Join(result.Directory, "nested")); err == nil {
		t.Fatal("backup directory used as restore destination")
	}
}

func TestMissingMediaIsExplicitAndCannotReadAsAvailable(t *testing.T) {
	doc, _, objectID := installation(t)
	ctx := context.Background()
	if err := os.Remove(filepath.Join(doc.Config.MediaRoot, "objects", objectID)); err != nil {
		t.Fatal(err)
	}
	b, err := backup.Create(ctx, doc, t.TempDir())
	if err != nil || b.Manifest.MissingMedia != 1 {
		t.Fatal("missing object was concealed", err)
	}
	r, err := backup.Restore(ctx, b.Directory, filepath.Join(t.TempDir(), "restored"))
	if err != nil || r.MissingMedia != 1 {
		t.Fatal(err)
	}
	store, err := sqlite.OpenMaintenance(ctx, filepath.Join(r.Directory, "data", "musicfs.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	object, err := store.ContentObject(ctx, "admin", objectID)
	if err != nil || object.State != "unavailable" {
		t.Fatalf("missing content still available: %+v %v", object, err)
	}
}

func TestBackupRejectsCorruptionOmittedReferencesAndLiveDirectory(t *testing.T) {
	doc, _, _ := installation(t)
	ctx := context.Background()
	if _, err := backup.Create(ctx, doc, doc.Config.DataRoot); err == nil {
		t.Fatal("overlapping backup accepted")
	}
	b, err := backup.Create(ctx, doc, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(b.Directory, "configuration.source.yaml")
	if err = os.WriteFile(file, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "invalid")
	if _, err = backup.Restore(ctx, b.Directory, destination); err == nil {
		t.Fatal("corrupt backup restored")
	}
	if _, err = os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("invalid backup created destination")
	}
	b, err = backup.Create(ctx, doc, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(b.Directory, "bundle.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest backup.Manifest
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Files = append(manifest.Files, backup.File{Path: "state/providers/../../outside", Size: 0, SHA256: strings.Repeat("0", 64)})
	raw, _ = json.Marshal(manifest)
	if err = os.WriteFile(manifestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = backup.Verify(ctx, b.Directory); err == nil {
		t.Fatal("invalid relative path accepted")
	}
	// Source configuration is checked again after copying, rather than trusting a
	// previously parsed revision across an external manual edit.
	changed := doc.Config
	changed.Listen = "127.0.0.1:8081"
	raw, _ = yaml.Marshal(changed)
	if err = os.WriteFile(doc.Path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = backup.Create(ctx, doc, t.TempDir()); err == nil {
		t.Fatal("stale source configuration accepted")
	}
}
