package library_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
)

func TestRootStatisticsIncludesConfiguredEmptyRootsWithoutOpeningWorker(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "statistics.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service := library.New(store, jobs.New(store), []library.Runtime{{ID: "local", Owner: "admin",
		RootNames: map[string]string{"b": "Empty", "a": "Not scanned"}}}, nil)
	result, err := service.RootStatistics(ctx, "admin", "local")
	want := []library.RootStatistic{{RootID: "a"}, {RootID: "b"}}
	if err != nil || !reflect.DeepEqual(result.Roots, want) {
		t.Fatalf("empty configured root statistics = %+v, want %+v: %v", result.Roots, want, err)
	}
	for _, scope := range [][2]string{{"other", "local"}, {"admin", "missing"}} {
		if _, err := service.RootStatistics(ctx, scope[0], scope[1]); err == nil {
			t.Fatal("unauthorized root statistics were allowed")
		}
	}
}
