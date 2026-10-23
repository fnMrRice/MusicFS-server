package library_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

func TestMetadataRefreshReprobesUnchangedAssetsAndKeepsOtherRoots(t *testing.T) {
	const instance = "b1548e96-3081-5c9c-8376-bf290e369d51"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	roots := []sourceaccess.RootConfig{{ID: "one", Path: t.TempDir()}, {ID: "two", Path: t.TempDir()}}
	for _, root := range roots {
		if err := os.WriteFile(filepath.Join(root.Path, root.ID+".wav"), []byte("unchanged source"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "scan.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	worker, err := pluginhost.Start(ctx, pluginhost.Config{Builtin: true, InstanceID: instance, UserID: "admin", StateDir: t.TempDir(), Roots: roots})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	var calls atomic.Int32
	probe := func(context.Context, io.ReadSeeker, int64) (library.Observation, error) {
		n := calls.Add(1)
		title, album := "Original title", "Original album"
		if n > 2 {
			title, album = "Refreshed title", "Refreshed album"
		}
		return library.Observation{Title: &title, Album: &album, Artists: []string{title}}, nil
	}
	declaration := worker.Manifest.Configuration
	defaults := declaration.ImportDefaults
	defaults.ImportLyrics, defaults.ImportArtwork = false, false
	queue := jobs.New(store)
	lib := library.New(store, queue, []library.Runtime{{ID: instance, Owner: "admin", Worker: worker, ConfigRevision: "refresh-test", Configuration: declaration, ImportDefaults: defaults, RootImportDefaults: map[string]pluginproto.ImportOptions{"one": defaults}}}, probe)
	if err := queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	input := library.ScanInput{Mode: "full", SchemaRevision: declaration.SchemaRevision, RootIDs: []string{"one", "two"}}
	initial, _, err := lib.Scan(ctx, "admin", instance, input, "refresh-initial")
	if err != nil || waitScanResult(t, ctx, lib, initial.ID).Status != "succeeded" {
		t.Fatal("initial scan failed", err)
	}
	before, err := lib.List(ctx, "admin", instance, "", 10)
	if err != nil || len(before.Items) != 2 || calls.Load() != 2 {
		t.Fatal("initial scan did not probe both roots", err)
	}
	input.RootIDs, input.RefreshMetadata = []string{"one"}, true
	refresh, _, err := lib.Scan(ctx, "admin", instance, input, "refresh-single-root")
	if err != nil {
		t.Fatal(err)
	}
	completed := waitScanResult(t, ctx, lib, refresh.ID)
	if completed.Status != "succeeded" || calls.Load() != 3 || !completed.EffectiveRootOptions["one"].ImportLyrics || !completed.EffectiveRootOptions["one"].ImportArtwork {
		t.Fatal("refresh did not force metadata for exactly one root", completed, calls.Load())
	}
	for _, previous := range before.Items {
		current, err := lib.Track(ctx, "admin", previous.ID)
		if err != nil || current.AssetID != previous.AssetID || current.Asset == nil || previous.Asset == nil || current.Asset.Version != previous.Asset.Version {
			t.Fatal("refresh changed the underlying asset", err)
		}
		want := "Original title"
		if previous.DisplayName == "one.wav" {
			want = "Refreshed title"
		}
		if current.Title == nil || *current.Title != want {
			t.Fatalf("refresh metadata scope mismatch: %+v", current)
		}
	}
	results, err := store.ScanAutomationResults(ctx, "admin", refresh.ID, "", 200)
	if err != nil || len(results.Items) != 1 || results.Items[0].Reason != "refreshMetadata" {
		t.Fatal("refresh trigger was not published with its source", results, err)
	}
	input.RefreshMetadata = false
	plan, err := lib.BuildScanPlan(ctx, "admin", instance, input)
	if err != nil || plan.Options.ImportLyrics || plan.Options.ImportArtwork || plan.RootOptions["one"].ImportLyrics || plan.RootOptions["one"].ImportArtwork {
		t.Fatal("refresh mutated saved defaults", plan, err)
	}
}
