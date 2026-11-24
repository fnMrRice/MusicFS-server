package plugindb_test

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/content/asmr"
	"github.com/fnMrRice/MusicFS-server/plugins/content/radio"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/state"
	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
)

func TestContentHistoriesIsolateAccountsAndReplay(t *testing.T) {
	ctx, path := t.Context(), filepath.Join(t.TempDir(), "plugins.sqlite")
	open := func(plugin string) *state.Store {
		db, err := state.Open(ctx, path, plugin, "b1548e96-3081-5c9c-8376-bf290e369d51")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		return db
	}
	voice := asmr.History(open(asmr.Descriptor().PluginID))
	episodes := radio.History(open(radio.Descriptor().PluginID))
	one := contenttype.HistoryEntry{WorkID: "one", WorkTitle: "Series", TrackTitle: "First", SourceTrackID: "s1", RecordedAt: time.Now().UTC()}
	two := one
	two.TrackTitle, two.SourceTrackID, two.RecordedAt = "Second", "s2", one.RecordedAt.Add(time.Second)
	var running sync.WaitGroup
	for _, item := range []contenttype.HistoryEntry{two, one, two} {
		running.Go(func() {
			for _, h := range []contenttype.History{voice, episodes} {
				if err := h.Record(ctx, "alice", item); err != nil {
					t.Error(err)
				}
			}
		})
	}
	running.Wait()
	check := func(h contenttype.History, owner string, want ...string) {
		t.Helper()
		items, err := h.List(ctx, owner)
		if err != nil || len(items) != len(want) {
			t.Fatalf("unexpected history: %+v %v", items, err)
		}
		for i, id := range want {
			if items[i].SourceTrackID != id {
				t.Fatalf("unexpected order: %+v", items)
			}
		}
	}
	check(voice, "alice", "s2")
	check(radio.History(open(radio.Descriptor().PluginID)), "alice", "s2", "s1")
	check(voice, "bob")
}
