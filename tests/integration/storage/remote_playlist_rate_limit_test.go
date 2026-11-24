package storage_test

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

func TestRemotePlaylistRateLimitedFavoritesPublishAll771Tracks(t *testing.T) {
	s, b := evidenceStore(t), remoteBinding()
	b.AccountID = "account"
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	link, err := s.LinkPlaylist(ctx, "owner", b, personal.Favorites)
	if err != nil {
		t.Fatal(err)
	}
	throttled, resume := make(chan struct{}), make(chan struct{})
	var reads atomic.Int32
	remote := &playlistRemote{binding: b, observe: func(ctx context.Context) (playlists.Observation, error) {
		if reads.Add(1) == 1 {
			close(throttled)
			return playlists.Observation{}, domain.Fail("RATE_LIMITED", "Too many requests")
		}
		select {
		case <-ctx.Done():
			return playlists.Observation{}, ctx.Err()
		case <-resume:
		}
		ids := make([]string, 771)
		for i := range ids {
			ids[i] = strconv.Itoa(i + 1)
		}
		return remoteObservation(1000, ids...), nil
	}}
	service, err := playlists.New(s, remote, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	changes := make(chan struct{}, 1)
	service.OnJobChanged(func(string) {
		select {
		case changes <- struct{}{}:
		default:
		}
	})
	p, _, err := service.Import(ctx, "owner", b.RemoteRef, "rate-limited-favorites")
	if err != nil || p.JobID != link.JobID {
		t.Fatalf("import did not reuse linked job: %+v %v", p, err)
	}
	if err = service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-throttled:
	case <-ctx.Done():
		t.Fatal("first read did not start")
	}
	running, err := service.GetJob(ctx, "owner", p.JobID)
	if err != nil || running.State != "running" {
		t.Fatalf("cooldown lost running task: %+v %v", running, err)
	}
	replay, merged, err := service.Import(ctx, "owner", b.RemoteRef, "rate-limited-favorites")
	if err != nil || !merged || replay.JobID != p.JobID {
		t.Fatalf("import replay changed job: %+v %v", replay, err)
	}
	before, err := service.Get(ctx, "owner", p.ID)
	if err != nil || before.Revision != 0 {
		t.Fatalf("partial observation was published: %+v %v", before, err)
	}
	close(resume)
	for {
		job, err := service.GetJob(ctx, "owner", p.JobID)
		if err != nil {
			t.Fatal(err)
		}
		if job.State == "succeeded" && job.Generation == running.Generation {
			break
		}
		if job.State != "running" {
			t.Fatalf("unexpected task result: %+v", job)
		}
		select {
		case <-changes:
		case <-ctx.Done():
			t.Fatal("sync did not finish", ctx.Err())
		}
	}
	page, err := service.Snapshot(ctx, "owner", p.ID, 1, 700, 100)
	if err != nil || page.Total != 771 || len(page.Items) != 71 || page.Items[70].Source.ExternalID != "771" || reads.Load() != 2 {
		t.Fatalf("remote favorite members missing: total=%d reads=%d err=%v", page.Total, reads.Load(), err)
	}
	favorite, err := s.Playlist(ctx, "owner", personal.Favorites)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.PlaylistSnapshot(ctx, "owner", personal.Favorites, favorite.SnapshotRevision)
	if err != nil || len(snapshot.Entries) != 771 {
		t.Fatalf("local favorites missing: count=%d err=%v", len(snapshot.Entries), err)
	}
	for _, entry := range snapshot.Entries {
		if entry.Source.TrackID == nil {
			t.Fatal("imported favorite is missing its library track")
		}
	}
	if addition, err := s.NextPlaylistAddition(ctx); err != nil || addition != nil {
		t.Fatalf("import echoed back to remote platform: %+v %v", addition, err)
	}
}

func TestRemotePlaylistRetryRechecksChangedInstance(t *testing.T) {
	s, ctx := evidenceStore(t), t.Context()
	remote := &playlistRemote{binding: remoteBinding()}
	remote.observe = func(context.Context) (playlists.Observation, error) {
		remote.mu.Lock()
		remote.binding.ConfigRevision = "new-revision"
		remote.mu.Unlock()
		return playlists.Observation{}, domain.Fail("RATE_LIMITED", "Too many requests")
	}
	service, err := playlists.New(s, remote, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	p, _, err := service.Import(ctx, "owner", remoteBinding().RemoteRef, "rate-limited-old-runtime")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	failed := remoteAwait(t, service, p.JobID, "failed")
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if failed.Error == nil || failed.Error.Code != "RESOURCE_CHANGED" || remote.calls != 1 {
		t.Fatalf("changed runtime reused: job=%+v reads=%d", failed, remote.calls)
	}
}

func TestRemotePlaylistFullFavoritesAreNotPlatformRateLimited(t *testing.T) {
	s, ctx, b := evidenceStore(t), t.Context(), remoteBinding()
	b.AccountID = "account"
	_, err := s.DB.ExecContext(ctx, `WITH RECURSIVE tracks(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM tracks WHERE n<?)
		INSERT INTO music_tracks(id,owner,title) SELECT 'local-'||n,'owner','Local '||n FROM tracks`, personal.MaxFavorites)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO music_playlist_entries(owner,playlist_id,binding_id,entry_id,track_id,position)
		SELECT owner,'favorites','',id,id,0 FROM music_tracks WHERE owner='owner'`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.LinkPlaylist(ctx, "owner", b, personal.Favorites); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimRemoteSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.FinishRemoteSync(ctx, job, remoteObservation(1000, "new-member"))
	matchingError(t, err, "UNSUPPORTED")
	current, err := s.RemotePlaylist(ctx, "owner", job.PlaylistID)
	if err != nil || current.Revision != 0 {
		t.Fatal("full favorites published a partial remote snapshot", err)
	}
}
