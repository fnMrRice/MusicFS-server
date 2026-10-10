package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/playlists"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
)

type leasedPlaylistRemote struct {
	*playlistRemote
	gate *plugincontrol.Gate
}

func (r leasedPlaylistRemote) Acquire(ctx context.Context, _ string, _ playlists.Binding) (context.Context, func(), error) {
	lease, err := r.gate.Acquire(ctx)
	if err != nil {
		return ctx, nil, err
	}
	return lease.Context(), lease.Close, nil
}

type finishingPlaylistStore struct {
	*sqlite.Store
	finish func(context.Context)
	fail   func(context.Context)
}

func (s finishingPlaylistStore) FinishRemoteSync(ctx context.Context, job playlists.SyncJob, observation playlists.Observation) (playlists.Snapshot, error) {
	if s.finish != nil {
		s.finish(ctx)
	}
	return s.Store.FinishRemoteSync(ctx, job, observation)
}
func (s finishingPlaylistStore) FailRemoteSync(ctx context.Context, job playlists.SyncJob, failure *domain.Error) error {
	if s.fail != nil {
		s.fail(ctx)
	}
	return s.Store.FailRemoteSync(ctx, job, failure)
}

func awaitPlaylistSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("playlist worker did not reach lifecycle boundary")
	}
}

func TestRemotePlaylistDrainHoldsWholeTaskLeaseThroughSnapshotPublication(t *testing.T) {
	store := evidenceStore(t)
	gate := plugincontrol.NewGate(true)
	observing, observed, publishing, publish := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	nestedError := make(chan error, 1)
	remote := leasedPlaylistRemote{gate: gate, playlistRemote: &playlistRemote{binding: remoteBinding(), observe: func(ctx context.Context) (playlists.Observation, error) {
		close(observing)
		select {
		case <-observed:
		case <-ctx.Done():
			return playlists.Observation{}, ctx.Err()
		}
		child, err := gate.Acquire(ctx)
		nestedError <- err
		if child != nil {
			child.Close()
		}
		return remoteObservation(1000, "42"), err
	}}}
	repo := finishingPlaylistStore{Store: store, finish: func(ctx context.Context) {
		close(publishing)
		select {
		case <-publish:
		case <-ctx.Done():
		}
	}}
	service, err := playlists.New(repo, remote, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	p, _, err := service.Import(context.Background(), "owner", remoteBinding().RemoteRef, "import-drain-task")
	if err != nil {
		t.Fatal(err)
	}
	job, _, err := service.Sync(context.Background(), "owner", p.ID, 1, "sync-drain-task")
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitPlaylistSignal(t, observing)
	gate.SetAccepting(false, false)
	close(observed)
	awaitPlaylistSignal(t, publishing)
	if err = <-nestedError; err != nil || gate.Active() != 1 {
		t.Fatal("drain lost nested admission or task lease before publication", err, gate.Active())
	}
	close(publish)
	remoteAwait(t, service, job.ID, "succeeded")
	service.Close()
	if gate.Active() != 0 {
		t.Fatal("completed snapshot publication leaked lease")
	}
}

func TestRemotePlaylistForceRetainsLeaseUntilFailureIsRecorded(t *testing.T) {
	store := evidenceStore(t)
	gate := plugincontrol.NewGate(true)
	observing, failing, finalize := make(chan struct{}), make(chan struct{}), make(chan struct{})
	remote := leasedPlaylistRemote{gate: gate, playlistRemote: &playlistRemote{binding: remoteBinding(), observe: func(ctx context.Context) (playlists.Observation, error) {
		close(observing)
		<-ctx.Done()
		return playlists.Observation{}, ctx.Err()
	}}}
	repo := finishingPlaylistStore{Store: store, fail: func(ctx context.Context) {
		close(failing)
		select {
		case <-finalize:
		case <-ctx.Done():
		}
	}}
	service, err := playlists.New(repo, remote, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	p, _, err := service.Import(context.Background(), "owner", remoteBinding().RemoteRef, "import-force-task")
	if err != nil {
		t.Fatal(err)
	}
	job, _, err := service.Sync(context.Background(), "owner", p.ID, 1, "sync-force-task")
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	awaitPlaylistSignal(t, observing)
	gate.SetAccepting(false, true)
	awaitPlaylistSignal(t, failing)
	if gate.Active() != 1 {
		t.Fatal("force released task before durable failure recording")
	}
	close(finalize)
	failed := remoteAwait(t, service, job.ID, "failed")
	service.Close()
	if failed.Error == nil || failed.Error.Code != "CANCELED" || gate.Active() != 0 {
		t.Fatal("forced synchronization did not finish cleanup", failed, gate.Active())
	}
	current, err := service.Get(context.Background(), "owner", p.ID)
	if err != nil || current.Revision != 0 || !current.Stale {
		t.Fatal("canceled observation published a snapshot", current, err)
	}
}
