package storage_test

import (
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
)

func TestMusicSourcePluginIdentityIsStable(t *testing.T) {
	ctx := t.Context()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "library.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	providers := map[string]string{"instance": "local"}
	for range 2 {
		if err := store.BindMusicSourcePlugins(ctx, providers); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.BindMusicSourcePlugins(ctx, map[string]string{"instance": "online", "another": "local"}); err == nil {
		t.Fatal("reassigning an instance to another plugin succeeded")
	}
	var count int
	if err := store.DB.QueryRowContext(ctx, "SELECT count(*) FROM music_source_providers").Scan(&count); err != nil || count != 1 {
		t.Fatalf("unexpected provider count after conflicting batch: %d %v", count, err)
	}
}
