package library_test

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

func TestScanReportsEachPageBeforeSlowMetadataAndPublishesOnlyAtCompletion(t *testing.T) {
	const instance = "b1548e96-3081-5c9c-8376-bf290e369d51"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := t.TempDir()
	for i := range 201 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("%03d.wav", i)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "scan.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	worker, err := pluginhost.Start(ctx, pluginhost.Config{Builtin: true, InstanceID: instance, UserID: "admin", StateDir: t.TempDir(), Roots: []sourceaccess.RootConfig{{ID: "music", Path: root}}})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	entered, release := make(chan int), make(chan struct{})
	processed := 0
	probe := func(ctx context.Context, _ io.ReadSeeker, _ int64) (library.Observation, error) {
		processed++
		if processed == 1 || processed == 201 {
			select {
			case entered <- processed:
			case <-ctx.Done():
				return library.Observation{}, ctx.Err()
			}
			select {
			case <-release:
			case <-ctx.Done():
				return library.Observation{}, ctx.Err()
			}
		}
		return library.Observation{}, nil
	}
	declaration := worker.Manifest.Configuration
	queue := jobs.New(store)
	lib := library.New(store, queue, []library.Runtime{{ID: instance, Owner: "admin", Worker: worker, ConfigRevision: "progress-test", Configuration: declaration, ImportDefaults: declaration.ImportDefaults}}, probe)
	if err := queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	job, _, err := lib.Scan(ctx, "admin", instance, library.ScanInput{Mode: "full", SchemaRevision: declaration.SchemaRevision, RootIDs: []string{"music"}}, "scan-progress")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []int{200, 201} {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("metadata probe did not start", ctx.Err())
		}
		current, err := lib.Job(ctx, "admin", job.ID)
		if err != nil || current.Status != "running" || current.Complete || current.Progress.Observed != want {
			t.Fatalf("page progress unavailable during metadata probe: %+v %v", current, err)
		}
		catalog, err := lib.List(ctx, "admin", instance, "", 10)
		if err != nil || len(catalog.Items) != 0 || catalog.LibraryRevision != "0" {
			t.Fatal("progress published an incomplete library", err)
		}
		select {
		case release <- struct{}{}:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	completed := waitScanResult(t, ctx, lib, job.ID)
	if completed.Status != "succeeded" || !completed.Complete || completed.Progress.Observed != 201 {
		t.Fatalf("scan did not complete: %+v", completed)
	}
	catalog, err := lib.List(ctx, "admin", instance, "", 200)
	if err != nil || len(catalog.Items) != 200 || catalog.LibraryRevision == "0" {
		t.Fatal("completed scan was not published", err)
	}
}
