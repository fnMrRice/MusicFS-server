package storage_test

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/library/recommendations"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

func TestRecommendationPagesSurviveRestartAndOnlyKeepFivePreviousResults(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "musicfs.sqlite")
	open := func(t *testing.T) *sqlite.Store {
		t.Helper()
		store, err := sqlite.Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { store.Close() })
		return store
	}
	const owner, scope = "admin", "scope-a"
	t.Run("publish", func(t *testing.T) {
		store := open(t)
		for i := 1; i <= 8; i++ {
			page := []recommendations.Source{{Name: strconv.Itoa(i)}}
			depth, err := store.SaveRecommendationPage(ctx, owner, scope, "2026-11-20", page, true)
			if err != nil || depth != min(i-1, 5) {
				t.Fatalf("save page %d: depth %d, error %v", i, depth, err)
			}
		}
		for range 2 {
			depth, err := store.SaveRecommendationPage(ctx, owner, scope, "2026-11-21", []recommendations.Source{{Name: "retry"}}, false)
			if err != nil || depth != 5 {
				t.Fatalf("save failure: depth %d, error %v", depth, err)
			}
		}
	})
	options := storageWorker(t)
	options.DataRoot = filepath.Dir(path)
	store, err := storage.Open(ctx, "plugin", options)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i, want := range []string{"retry", "7", "6", "5", "4", "3"} {
		page, err := store.ReadRecommendationPage(ctx, owner, scope, i)
		if err != nil || page.HistoryDepth != 5 || len(page.Sources) != 1 || page.Sources[0].Name != want {
			t.Fatalf("history %d: unexpected page %+v, error %v", i, page, err)
		}
		if i == 0 && page.Day != "2026-11-21" {
			t.Fatalf("lost last attempted server day: %s", page.Day)
		}
	}
	for _, key := range [][2]string{{"other", scope}, {owner, "scope-b"}} {
		page, err := store.ReadRecommendationPage(ctx, key[0], key[1], 0)
		if err != nil || page.Sources != nil {
			t.Fatalf("unexpected page for another scope: %+v, error %v", page, err)
		}
	}
	depth, err := store.SaveRecommendationPage(ctx, owner, scope, "2026-11-21", []recommendations.Source{{Name: "new"}}, true)
	if err != nil || depth != 5 {
		t.Fatalf("publish after restart: depth %d, error %v", depth, err)
	}
	page, err := store.ReadRecommendationPage(ctx, owner, scope, 1)
	if err != nil || len(page.Sources) != 1 || page.Sources[0].Name != "8" {
		t.Fatalf("the previous successful page was not retained: %+v, error %v", page, err)
	}
}
