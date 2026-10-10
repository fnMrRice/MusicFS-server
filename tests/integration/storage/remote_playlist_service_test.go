package storage_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/playlists"
)

type playlistRemote struct {
	mu      sync.Mutex
	binding playlists.Binding
	observe func(context.Context) (playlists.Observation, error)
	calls   int
}

func (r *playlistRemote) Authorize(_ context.Context, owner string, ref playlists.RemoteRef) (playlists.Binding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if owner != "owner" || ref.InstanceID != r.binding.InstanceID {
		return playlists.Binding{}, domain.Fail("NOT_FOUND", "Online instance not found")
	}
	if ref.AccountID != "" {
		return playlists.Binding{}, domain.Fail("UNSUPPORTED", "Account access is unavailable")
	}
	b := r.binding
	b.RemoteRef = ref
	return b, nil
}
func (r *playlistRemote) ObservePlaylist(ctx context.Context, _ string, _ playlists.Binding) (playlists.Observation, error) {
	r.mu.Lock()
	r.calls++
	fn := r.observe
	r.mu.Unlock()
	return fn(ctx)
}
func remoteAwait(t *testing.T, s *playlists.Service, id, state string) playlists.SyncJob {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for ctx.Err() == nil {
		j, err := s.GetJob(ctx, "owner", id)
		if err != nil {
			t.Fatal(err)
		}
		if j.State == state {
			return j
		}
		if j.State == "failed" {
			t.Fatal("unexpected task failure", j.Error)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("playlist task did not reach state", state)
	return playlists.SyncJob{}
}

func TestRemotePlaylistServiceExplicitImportAndBackgroundSync(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	remote := &playlistRemote{binding: remoteBinding(), observe: func(context.Context) (playlists.Observation, error) {
		return remoteObservation(1000, "a", "a", "b"), nil
	}}
	service, err := playlists.New(s, remote, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	p, _, err := service.Import(ctx, "owner", remoteBinding().RemoteRef, "import-explicit")
	if err != nil {
		t.Fatal(err)
	}
	remote.mu.Lock()
	calls := remote.calls
	remote.mu.Unlock()
	if calls != 0 || p.Revision != 0 {
		t.Fatal("import made an implicit network request")
	}
	if _, _, err = service.Import(ctx, "stranger", remoteBinding().RemoteRef, "import-forbidden"); err == nil {
		t.Fatal("unauthorized import allowed")
	}
	account := remoteBinding().RemoteRef
	account.AccountID = "not-supported"
	_, _, err = service.Import(ctx, "owner", account, "import-no-account")
	matchingError(t, err, "UNSUPPORTED")
	j, _, err := service.Sync(ctx, "owner", p.ID, 1, "sync-explicit")
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	remoteAwait(t, service, j.ID, "succeeded")
	page, err := service.Snapshot(ctx, "owner", p.ID, 1, 0, 2)
	if err != nil || len(page.Items) != 2 || page.NextOffset == nil {
		t.Fatal(err)
	}
	remote.mu.Lock()
	remote.observe = func(ctx context.Context) (playlists.Observation, error) {
		<-ctx.Done()
		return playlists.Observation{}, ctx.Err()
	}
	remote.mu.Unlock()
	j, _, err = service.Sync(ctx, "owner", p.ID, 1, "sync-cancelled")
	if err != nil {
		t.Fatal(err)
	}
	remoteAwait(t, service, j.ID, "running")
	if _, err = service.Cancel(ctx, "owner", j.ID); err != nil {
		t.Fatal(err)
	}
	remoteAwait(t, service, j.ID, "cancelled")
	retained, err := service.Get(ctx, "owner", p.ID)
	if err != nil || retained.Revision != 1 || !retained.Stale {
		t.Fatal("cancel removed successful snapshot", err)
	}
}

func TestRemotePlaylistServiceRechecksRuntimeBeforeReading(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	remote := &playlistRemote{binding: remoteBinding(), observe: func(context.Context) (playlists.Observation, error) { return remoteObservation(1000, "a"), nil }}
	service, err := playlists.New(s, remote, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	p, _, err := service.Import(ctx, "owner", remoteBinding().RemoteRef, "import-runtime-check")
	if err != nil {
		t.Fatal(err)
	}
	j, _, err := service.Sync(ctx, "owner", p.ID, 1, "sync-runtime-check")
	if err != nil {
		t.Fatal(err)
	}
	remote.mu.Lock()
	remote.binding.ConfigRevision = "config-changed"
	remote.mu.Unlock()
	if err = service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	failed := remoteAwait(t, service, j.ID, "failed")
	if failed.Error == nil || failed.Error.Code != "RESOURCE_CHANGED" {
		t.Fatal("stale runtime ticket did not fail")
	}
	remote.mu.Lock()
	calls := remote.calls
	remote.mu.Unlock()
	if calls != 0 {
		t.Fatal("changed runtime performed remote read")
	}
	current, err := service.Get(ctx, "owner", p.ID)
	if err != nil || current.Revision != 0 || !current.Stale {
		t.Fatal("failed runtime published data", err)
	}
}
