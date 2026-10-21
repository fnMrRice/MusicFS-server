package sourcedisplay_test

import (
	"context"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/library/sourcedisplay"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
)

type sourceRepo struct{ items []sourcedisplay.Item }

func (r sourceRepo) SourceDisplay(context.Context, string, []playlists.SourceRef) (sourcedisplay.Page, error) {
	return sourcedisplay.Page{Items: r.items}, nil
}

type batchProvider struct {
	onlinehost.Provider
	onlinehost.DisplayProvider
	calls int
}

func (p *batchProvider) MetadataBatch(_ context.Context, ids []string) (online.TrackBatch, error) {
	p.calls++
	batch := online.TrackBatch{Items: []online.TrackLookup{}}
	for _, id := range ids {
		batch.Items = append(batch.Items, online.TrackLookup{ExternalID: id, Track: &online.Track{ExternalID: id, Title: "Display"}})
	}
	return batch, nil
}
func TestLookupGroupsRemoteReferencesByInstanceWithoutPerTrackCalls(t *testing.T) {
	a, b := &batchProvider{}, &batchProvider{}
	host, err := onlinehost.New([]onlinehost.Runtime{{Owner: "owner", InstanceID: "a", Platform: online.Netease, ConfigRevision: "rev", Enabled: true, Provider: a}, {Owner: "owner", InstanceID: "b", Platform: online.Netease, ConfigRevision: "rev", Enabled: true, Provider: b}})
	if err != nil {
		t.Fatal(err)
	}
	items := []sourcedisplay.Item{}
	refs := []playlists.SourceRef{}
	for _, id := range []string{"a", "b", "a", "a", "b"} {
		refID := domain.NewID()
		ref := playlists.SourceRef{RemoteSourceID: &refID}
		refs = append(refs, ref)
		items = append(items, sourcedisplay.Item{Source: ref, RemoteSource: &playlists.RemoteSource{ID: refID, RemoteRef: playlists.RemoteRef{InstanceID: id, AccountID: "12345", ExternalID: "42"}}})
	}
	page, err := sourcedisplay.New(sourceRepo{items}, host).Lookup(context.Background(), "owner", refs)
	if err != nil || len(page.Items) != 5 || a.calls != 1 || b.calls != 1 {
		t.Fatal("per-track calls or ordering loss", page, err, a.calls, b.calls)
	}
	for i, item := range page.Items {
		if item.Source.Key() != refs[i].Key() || item.RemoteTrack == nil {
			t.Fatal("result association changed")
		}
	}
}

func (p *batchProvider) PlatformSession(context.Context) (online.AccountView, error) {
	return online.AccountView{State: "signedIn", AccountID: "12345", Revision: "fixture-generation"}, nil
}
