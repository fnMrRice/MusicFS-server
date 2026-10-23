package library_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/src/tasks"
)

func TestRefreshTaskCancellationWaitsForProbeAndPreventsPublication(t *testing.T) {
	const instance = "b1548e96-3081-5c9c-8376-bf290e369d51"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "song.wav"), []byte("probe source"), 0600); err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	worker, err := pluginhost.Start(ctx, pluginhost.Config{Builtin: true, InstanceID: instance, UserID: "owner", StateDir: t.TempDir(), Roots: []sourceaccess.RootConfig{{ID: "music", Path: root}}})
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	started, interrupted, released := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var release sync.Once
	probe := func(ctx context.Context, _ io.ReadSeeker, _ int64) (library.Observation, error) {
		close(started)
		<-ctx.Done()
		close(interrupted)
		<-released
		return library.Observation{}, ctx.Err()
	}
	queue := jobs.New(store)
	lib := library.New(store, queue, []library.Runtime{{ID: instance, Owner: "owner", Name: "Local music", RootNames: map[string]string{"music": "My music"}, Worker: worker, ConfigRevision: "tasks-test", Configuration: worker.Manifest.Configuration, ImportDefaults: worker.Manifest.Configuration.ImportDefaults}}, probe)
	if err := queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer queue.Close()
	defer release.Do(func() { close(released) })
	taskService := tasks.New(store, queue, nil, lib.TaskNames)
	defer taskService.Close()
	job, _, err := lib.Scan(ctx, "owner", instance, library.ScanInput{Mode: "full", SchemaRevision: worker.Manifest.Configuration.SchemaRevision, RootIDs: []string{"music"}, RefreshMetadata: true}, "refresh-task-cancel")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("probe did not start")
	}
	task, err := taskService.Cancel(ctx, "owner", job.ID)
	if err != nil || task.Status != "cancelling" || task.ProviderName != "Local music" || task.Roots[0].Name != "My music" {
		t.Fatal("cancellation was not recorded", task, err)
	}
	select {
	case <-interrupted:
	case <-ctx.Done():
		t.Fatal("probe was not interrupted")
	}
	still, err := taskService.Get(ctx, "owner", job.ID)
	if err != nil || still.Status != "cancelling" {
		t.Fatal("task completed before resource release", still, err)
	}
	page, err := taskService.List(ctx, "owner", "", 1)
	if err != nil || page.ActiveCount != 1 {
		t.Fatal("cancelling task missing from active count", page, err)
	}
	release.Do(func() { close(released) })
	for {
		task, err = taskService.Get(ctx, "owner", job.ID)
		if err != nil {
			t.Fatal(err)
		}
		if task.Status == "cancelled" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("cancellation did not complete")
		case <-time.After(10 * time.Millisecond):
		}
	}
	tracks, err := lib.List(ctx, "owner", instance, "", 10)
	if err != nil || len(tracks.Items) != 0 {
		t.Fatal("cancelled scan published tracks", tracks, err)
	}
	results, err := store.ScanAutomationResults(ctx, "owner", job.ID, "", 50)
	if err != nil || len(results.Items) != 0 {
		t.Fatal("cancelled scan queued metadata", results, err)
	}
	page, err = taskService.List(ctx, "owner", "", 1)
	if err != nil || page.ActiveCount != 0 {
		t.Fatal("released task is still active", page, err)
	}
}
