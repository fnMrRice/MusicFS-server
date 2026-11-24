package onlinehost_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"testing/synctest"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
)

type limitedPlaylist struct {
	observedPlaylist
	onlinehost.DisplayProvider
	revision    string
	changeLogin bool
	limited     bool
	batches     map[string]int
	slot        chan struct{}
}

func (p *limitedPlaylist) PlatformSession(context.Context) (online.AccountView, error) {
	return online.AccountView{State: "signedIn", AccountID: "12345", Revision: p.revision}, nil
}

func (p *limitedPlaylist) Metadata(ctx context.Context, id string) (online.Track, error) {
	select {
	case <-ctx.Done():
		return online.Track{}, ctx.Err()
	case <-p.slot:
		return online.Track{ExternalID: id}, nil
	}
}

func (p *limitedPlaylist) ObservePlaylist(_ context.Context, id string) (online.PlaylistObservation, error) {
	p.calls++
	if p.limited && p.calls == 1 {
		delay := 3
		return online.PlaylistObservation{}, &domain.Error{Code: "RATE_LIMITED", RetryAfter: &delay}
	}
	count := int64(771)
	entries := make([]online.PlaylistEntry, count)
	for i := range entries {
		entries[i] = online.PlaylistEntry{Position: i, ExternalTrackID: strconv.Itoa(i + 1)}
	}
	return online.PlaylistObservation{
		Playlist: online.Playlist{ExternalID: id, Title: "Favorites", TrackCount: &count},
		Entries:  entries, Complete: true, Consistency: online.Unversioned, ObservedAt: time.Now(),
	}, nil
}

func (p *limitedPlaylist) MetadataBatch(_ context.Context, ids []string) (online.TrackBatch, error) {
	first := ids[0]
	p.batches[first]++
	if p.limited && first == "701" && p.batches[first] == 1 {
		if p.changeLogin {
			p.revision = "new-login"
		}
		delay := 60
		return online.TrackBatch{}, &domain.Error{Code: "RATE_LIMITED", RetryAfter: &delay}
	}
	items := make([]online.TrackLookup, len(ids))
	for i, id := range ids {
		items[i] = online.TrackLookup{ExternalID: id, Track: &online.Track{ExternalID: id, Title: "Track"}}
	}
	return online.TrackBatch{Items: items}, nil
}

func newLimitedPlaylist(t *testing.T) (*onlinehost.Service, *limitedPlaylist, playlists.Binding) {
	t.Helper()
	p := &limitedPlaylist{revision: "login", batches: map[string]int{}, slot: make(chan struct{})}
	host, err := onlinehost.New([]onlinehost.Runtime{{Owner: "owner", InstanceID: "net", Platform: online.Netease, ConfigRevision: "config", Enabled: true, Provider: p}})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := host.PlaylistRemote().Authorize(t.Context(), "owner", playlists.RemoteRef{InstanceID: "net", AccountID: "12345", ExternalID: "91"})
	if err != nil {
		t.Fatal(err)
	}
	return host, p, binding
}

func TestPlaylistReadRetriesOnlyLimitedPartOf771Tracks(t *testing.T) {
	for _, name := range []string{"same login", "new login for same account"} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				host, p, binding := newLimitedPlaylist(t)
				p.limited, p.changeLogin = true, name != "same login"
				start := time.Now()
				got, err := host.PlaylistRemote().ObservePlaylist(t.Context(), "owner", binding)
				if p.changeLogin {
					var failure *domain.Error
					if !errors.As(err, &failure) || failure.Code != "RESOURCE_CHANGED" || p.batches["701"] != 1 || p.batches["751"] != 0 || len(got.Tracks) != 0 {
						t.Fatalf("observation mixed login generations: %d tracks, batches=%v, err=%v", len(got.Tracks), p.batches, err)
					}
					return
				}
				if err != nil || len(got.Tracks) != 771 || p.calls != 2 || len(p.batches) != 16 || time.Since(start) != 63*time.Second {
					t.Fatalf("observation: %d tracks, %d observations, batches=%v, elapsed=%s, err=%v", len(got.Tracks), p.calls, p.batches, time.Since(start), err)
				}
				for i, item := range got.Tracks {
					if item.ExternalID != strconv.Itoa(i+1) {
						t.Fatalf("unexpected track %q at index %d", item.ExternalID, i)
					}
				}
				for first, calls := range p.batches {
					if first == "701" && calls != 2 || first != "701" && calls != 1 {
						t.Fatalf("unexpected batch requests: %v", p.batches)
					}
				}
			})
		})
	}
}

func TestPlaylistObservationWaitsForOnlineHostCapacity(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		host, p, binding := newLimitedPlaylist(t)
		for range 4 {
			go func() { _, _ = host.Metadata(t.Context(), "owner", "net", "1") }()
		}
		synctest.Wait()
		time.AfterFunc(500*time.Millisecond, func() { close(p.slot) })
		start := time.Now()
		got, err := host.PlaylistRemote().ObservePlaylist(t.Context(), "owner", binding)
		if err != nil || len(got.Tracks) != 771 || time.Since(start) != time.Second || p.calls != 1 {
			t.Fatalf("host slot contention stopped observation: %d tracks, %d calls, elapsed=%s, err=%v", len(got.Tracks), p.calls, time.Since(start), err)
		}
	})
}
