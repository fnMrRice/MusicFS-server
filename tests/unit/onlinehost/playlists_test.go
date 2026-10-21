package onlinehost_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
)

type observedPlaylist struct {
	onlinehost.Provider
	calls int
}

func (p *observedPlaylist) ObservePlaylist(_ context.Context, id string) (online.PlaylistObservation, error) {
	p.calls++
	return online.PlaylistObservation{Playlist: online.Playlist{ExternalID: id}, Consistency: online.Unversioned, ObservedAt: time.Now().UTC()}, nil
}

func TestRemotePlaylistAdapterScopeAndConfigurationBinding(t *testing.T) {
	p := &observedPlaylist{}
	host, err := onlinehost.New([]onlinehost.Runtime{
		{Owner: "mine", InstanceID: "net", Platform: online.Netease, ConfigRevision: "current", Enabled: true, Provider: p},
		{Owner: "mine", InstanceID: "qq", Platform: online.QQMusic, ConfigRevision: "current", Enabled: true, Provider: p},
		{Owner: "mine", InstanceID: "off", Platform: online.Netease, ConfigRevision: "current", Provider: p},
	})
	if err != nil {
		t.Fatal(err)
	}
	r := host.PlaylistRemote()
	ctx := context.Background()
	ref := playlists.RemoteRef{InstanceID: "net", AccountID: "12345", ExternalID: "91"}
	binding, err := r.Authorize(ctx, "mine", ref)
	if err != nil || binding.RemoteRef != ref || binding.ConfigRevision != "current" || p.calls != 0 {
		t.Fatal("authorization changed scope or fetched data", err)
	}
	for _, tc := range []struct{ owner, instance, account, external, code string }{
		{"other", "net", "", "91", "NOT_FOUND"}, {"mine", "net", "other", "91", "ACCESS_DENIED"},
		{"mine", "qq", "12345", "91", "UNSUPPORTED"}, {"mine", "off", "", "91", "SOURCE_OFFLINE"},
		{"mine", "net", "", "https://private.invalid/91", "INVALID_ARGUMENT"}, {"mine", "net", "12345", "fake", "INVALID_ARGUMENT"},
	} {
		_, err := r.Authorize(ctx, tc.owner, playlists.RemoteRef{InstanceID: tc.instance, AccountID: tc.account, ExternalID: tc.external})
		var failure *domain.Error
		if !errors.As(err, &failure) || failure.Code != tc.code {
			t.Fatalf("authorize %s: %v", tc.instance, err)
		}
	}
	old := binding
	old.ConfigRevision = "old"
	if _, err := r.ObservePlaylist(ctx, "mine", old); err == nil || p.calls != 0 {
		t.Fatal("old configuration reached provider")
	}
	if _, err := r.ObservePlaylist(ctx, "other", binding); err == nil || p.calls != 0 {
		t.Fatal("other owner reached provider")
	}
	observation, err := r.ObservePlaylist(ctx, "mine", binding)
	if err != nil || observation.Version != nil || observation.Consistency != online.Unversioned || p.calls != 1 {
		t.Fatal("current anonymous observation failed", err)
	}
}

func (p *observedPlaylist) PlatformSession(context.Context) (online.AccountView, error) {
	return online.AccountView{State: "signedIn", AccountID: "12345", Revision: "fixture-generation"}, nil
}
