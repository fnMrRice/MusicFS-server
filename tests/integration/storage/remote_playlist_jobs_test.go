package storage_test

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/playlists"
)

func TestRemotePlaylistQueueMergesAndCancellationIsDurable(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	b := remoteBinding()
	p := remoteImport(t, s, "owner", b)
	var wg sync.WaitGroup
	results := make(chan playlists.SyncJob, 2)
	failures := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, _, err := s.QueueRemoteSync(ctx, "owner", p.ID, 1, b, domain.NewID())
			results <- j
			failures <- err
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	id := ""
	for j := range results {
		if id != "" && id != j.ID {
			t.Fatal("concurrent sync did not merge")
		}
		id = j.ID
	}
	running, err := s.ClaimRemoteSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := s.CancelRemoteSync(ctx, "owner", id)
	if err != nil || cancelled.State != "cancelled" {
		t.Fatal(err)
	}
	replay, err := s.CancelRemoteSync(ctx, "owner", id)
	if err != nil || !reflect.DeepEqual(cancelled, replay) {
		t.Fatal("cancel replay changed terminal task", err)
	}
	_, err = s.FinishRemoteSync(ctx, running, remoteObservation(1000, "a"))
	matchingError(t, err, "CONFLICT")
	_, err = s.RemoteSyncJob(ctx, "another-owner", id)
	matchingError(t, err, "NOT_FOUND")
	current, _ := s.RemotePlaylist(ctx, "owner", p.ID)
	if current.Revision != 0 {
		t.Fatal("cancelled task created a snapshot")
	}
}

func TestRemotePlaylistRecoveryAndTicketBindingRejectLatePublish(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	p := remoteImport(t, s, "owner", remoteBinding())
	old := remoteClaim(t, s, p)
	var sequence int
	var name, path string
	if err := s.DB.QueryRow("PRAGMA database_list").Scan(&sequence, &name, &path); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.RecoverRemoteSync(ctx); err != nil {
		t.Fatal(err)
	}
	newTicket, err := reopened.ClaimRemoteSync(ctx)
	if err != nil || newTicket.Generation <= old.Generation {
		t.Fatal("recovery reused generation", err)
	}
	_, err = s.FinishRemoteSync(ctx, old, remoteObservation(1000, "a"))
	matchingError(t, err, "CONFLICT")
	for _, change := range []func(*playlists.SyncJob){func(j *playlists.SyncJob) { j.Binding.AccountID = "other-account" }, func(j *playlists.SyncJob) { j.Binding.ExternalID = "456" }, func(j *playlists.SyncJob) { j.Binding.ConfigRevision = "changed" }, func(j *playlists.SyncJob) { j.PlaylistID = "other-playlist" }, func(j *playlists.SyncJob) { j.BindingRevision++ }} {
		forged := newTicket
		change(&forged)
		observation := remoteObservation(1000, "a")
		observation.Playlist.ExternalID = forged.Binding.ExternalID
		_, err = reopened.FinishRemoteSync(ctx, forged, observation)
		matchingError(t, err, "CONFLICT")
	}
	if _, err = reopened.FinishRemoteSync(ctx, newTicket, remoteObservation(1000, "a")); err != nil {
		t.Fatal(err)
	}
	_, err = reopened.FinishRemoteSync(ctx, newTicket, remoteObservation(1001, "b"))
	matchingError(t, err, "CONFLICT")
	stored, err := reopened.RemoteSnapshot(ctx, "owner", p.ID, 1)
	if err != nil || stored.Entries[0].Source.ExternalID != "a" {
		t.Fatal("late task replaced snapshot", err)
	}
}

func TestRemotePlaylistChangedConfigurationRevokesOldTask(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	p := remoteImport(t, s, "owner", remoteBinding())
	old := remoteClaim(t, s, p)
	b := remoteBinding()
	b.ConfigRevision = "config-v2"
	queued, merged, err := s.QueueRemoteSync(ctx, "owner", p.ID, 1, b, "config-new-task")
	if err != nil || merged || queued.BindingRevision != 2 {
		t.Fatal("new configuration did not create a new binding generation", err)
	}
	_, err = s.FinishRemoteSync(ctx, old, remoteObservation(1000, "a"))
	matchingError(t, err, "CONFLICT")
	_, _, err = s.QueueRemoteSync(ctx, "owner", p.ID, 1, b, domain.NewID())
	matchingError(t, err, "CONFLICT")
	replayed, merged, err := s.QueueRemoteSync(ctx, "owner", p.ID, 1, b, "config-new-task")
	if err != nil || !merged || replayed.ID != queued.ID {
		t.Fatal("enqueue replay failed after revision advancement", err)
	}
	j, err := s.ClaimRemoteSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.FinishRemoteSync(ctx, j, remoteObservation(1001, "a")); err != nil {
		t.Fatal(err)
	}
}

func TestRemotePlaylistFixedVersionRejectsContradictoryObservations(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	p := remoteImport(t, s, "owner", remoteBinding())
	version := "v1"
	observation := remoteObservation(1000, "a", "b")
	observation.Consistency = "versioned"
	observation.Version = &version
	first := remotePublish(t, s, p, observation)
	next := remoteObservation(1001, "a")
	next.Consistency = "versioned"
	next.Version = &version
	j := remoteClaim(t, s, p)
	_, err := s.FinishRemoteSync(ctx, j, next)
	matchingError(t, err, "RESOURCE_CHANGED")
	if err = s.FailRemoteSync(ctx, j, &domain.Error{Code: "RESOURCE_CHANGED"}); err != nil {
		t.Fatal(err)
	}
	newVersion := "v2"
	next.Version = &newVersion
	removed := remotePublish(t, s, p, next)
	if len(removed.Entries) != 1 || len(removed.Removals) != 1 || removed.Removals[0].EntryID != first.Entries[1].ID {
		t.Fatal("fixed complete version did not reconcile missing occurrence")
	}
}
