package backup_test

import (
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
	"github.com/fnMrRice/MusicFS-server/src/storage/backup"
)

func TestPortableRestoreClearsDSPPreferencesOnlyInRestoredInstallation(t *testing.T) {
	doc, _, _ := installation(t)
	path := filepath.Join(doc.Config.DataRoot, "musicfs.sqlite")
	store, err := sqlite.OpenMaintenance(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	options := dspsettings.Options{Chain: []audio.NodeSpec{{Name: "reverb", Version: 1}}, OutputLayout: "7.1", BufferFrames: 16384}
	records := []struct{ owner, id string }{
		{"admin", dspsettings.StorageID},
		{"other-user", dspsettings.StorageID},
		{dspsettings.GlobalOwner, dspsettings.GlobalStorageID},
		{"admin", "example.preferences"},
	}
	for _, row := range records {
		if _, err = store.PutPluginSettings(t.Context(), row.owner, row.id, 0, settings.Values{"schemaRevision": dspsettings.SchemaRevision, "settings": options}); err != nil {
			store.Close()
			t.Fatal(err)
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	bundle, err := backup.Create(t.Context(), doc, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	restored, err := backup.Restore(t.Context(), bundle.Directory, filepath.Join(t.TempDir(), "restored"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []struct {
		path     string
		restored bool
	}{{filepath.Join(restored.Directory, "data", "musicfs.sqlite"), true}, {path, false}} {
		database, err := sqlite.OpenMaintenance(t.Context(), entry.path)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range records {
			value, err := database.GetPluginSettings(t.Context(), row.owner, row.id)
			if err != nil {
				database.Close()
				t.Fatal(err)
			}
			wantRemoved := entry.restored && row.id != "example.preferences"
			if (value.Values == nil) != wantRemoved {
				database.Close()
				t.Fatal("restore changed the wrong preference record", entry.restored, row.owner, row.id, value)
			}
		}
		database.Close()
	}
}
