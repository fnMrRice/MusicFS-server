package storage_test

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/postgres"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/library/recommendations"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestRecommendationPagesSurviveRestartAndOnlyKeepFivePreviousResults(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "musicfs.sqlite")
	publish := func() (storage.Backend, error) { return sqlite.Open(ctx, path) }
	reopen := func() (storage.Backend, error) {
		options := storageWorker(t)
		options.DataRoot = filepath.Dir(path)
		return storage.Open(ctx, "plugin", options)
	}
	verifyRecommendationHistory(t, publish, reopen)
}

func TestPostgresRecommendationPagesSurviveRestart(t *testing.T) {
	options := storage.Options{DataRoot: t.TempDir(), Values: testkit.PostgresOptions(t)}
	open := func() (storage.Backend, error) { return postgres.Driver().Open(t.Context(), options) }
	verifyRecommendationHistory(t, open, open)
}

func verifyRecommendationHistory(t *testing.T, publish, reopen func() (storage.Backend, error)) {
	t.Helper()
	ctx := t.Context()
	const owner, scope = "admin", "scope-a"
	t.Run("publish", func(t *testing.T) {
		store, err := publish()
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
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
	store, err := reopen()
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
