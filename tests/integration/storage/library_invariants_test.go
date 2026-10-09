package storage_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
)

func TestSinglePageRefreshDoesNotExhaustSnapshots(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for i := 0; i < 100; i++ {
		if _, err := store.ListTracks(ctx, "admin", "", "", 50); err != nil {
			t.Fatalf("empty library refresh %d failed: %v", i, err)
		}
	}
	var retained int
	if err := store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM library_snapshots").Scan(&retained); err != nil || retained != 0 {
		t.Fatalf("unneeded snapshots retained: %d, %v", retained, err)
	}
}

func TestLibraryRejectsDuplicateObservationAtomically(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, _, err := store.Enqueue(ctx, "admin", "root", "scan-full", "duplicate-scan"); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	item := library.Observation{Observation: pluginproto.Observation{ExternalID: "one", RootID: "root", ObservedVersion: "v1", DisplayName: "a.wav"}}
	if err := store.Publish(ctx, job, []library.Observation{item, item}); err == nil {
		t.Fatal("duplicate identity was accepted")
	}
	page, err := store.ListTracks(ctx, "admin", "root", "", 50)
	if err != nil || len(page.Items) != 0 || page.LibraryRevision != "0" {
		t.Fatal("rejected publish left partial changes", err)
	}
}

func TestCursorRetryReusesCursorAndCorruptionFails(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, _, err := store.Enqueue(ctx, "admin", "root", "scan-full", "three-tracks"); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	items := []library.Observation{}
	for _, name := range []string{"a", "b", "c"} {
		items = append(items, library.Observation{Observation: pluginproto.Observation{ExternalID: name, RootID: "root", ObservedVersion: "v1", DisplayName: name}})
	}
	if err := store.Publish(ctx, job, items); err != nil {
		t.Fatal(err)
	}
	first, err := store.ListTracks(ctx, "admin", "root", "", 1)
	if err != nil || first.NextCursor == nil {
		t.Fatal("first page failed", err)
	}
	second, err := store.ListTracks(ctx, "admin", "root", *first.NextCursor, 1)
	if err != nil || second.NextCursor == nil {
		t.Fatal("second page failed", err)
	}
	retry, err := store.ListTracks(ctx, "admin", "root", *first.NextCursor, 1)
	if err != nil || retry.NextCursor == nil || *retry.NextCursor != *second.NextCursor {
		t.Fatal("cursor retry changed next cursor", err)
	}
	if _, err := store.DB.ExecContext(ctx, "UPDATE library_snapshot_items SET document='invalid' WHERE position=1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListTracks(ctx, "admin", "root", *first.NextCursor, 1); err == nil {
		t.Fatal("corrupt persisted snapshot silently became an empty track")
	}
}

func TestPublishChecksOwnerAndPreservesOldLibraryOnCorruption(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, _, err := store.Enqueue(ctx, "admin", "root", "scan-full", "owner-check"); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	forged := jobs.Job{ID: job.ID, Owner: "other", ProviderInstanceID: "root", Generation: job.Generation}
	if err := store.Publish(ctx, forged, nil); err == nil {
		t.Fatal("publish accepted a job with different ownership")
	}
	item := library.Observation{Observation: pluginproto.Observation{ExternalID: "one", RootID: "root", ObservedVersion: "v1", DisplayName: "a"}}
	if err := store.Publish(ctx, job, []library.Observation{item}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB.ExecContext(ctx, "UPDATE tracks SET document='invalid'"); err != nil {
		t.Fatal(err)
	}
	if err := store.Publish(ctx, job, nil); err == nil {
		t.Fatal("publish ignored corrupt previous track data")
	}
	var raw []byte
	if err := store.DB.QueryRowContext(ctx, "SELECT document FROM tracks").Scan(&raw); err != nil || json.Valid(raw) {
		t.Fatal("failed publish rewrote existing data", err)
	}
}
