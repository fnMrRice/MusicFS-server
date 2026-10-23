package onlinehost_test

import (
	"context"
	"errors"
	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
)

// 替身故意不实现任何账号接口，验证匿名策略不依赖隐藏的登录状态。
type anonymousBilibili struct {
	onlinehost.Provider
	calls int
}

func (p *anonymousBilibili) Descriptor() pluginproto.OnlineDescriptor {
	return onlineplugins.Describe(online.Bilibili)
}

func (p *anonymousBilibili) Metadata(_ context.Context, id string) (online.Track, error) {
	p.calls++
	track := online.Track{ExternalID: id, Title: "Public fixture", Artists: []string{}, SourceURL: online.ReferenceURL(onlineplugins.Describe(online.Bilibili).Resources.Tracks, id), PlaybackAvailability: "unknown"}
	track.MetadataDigest = online.Digest(track)
	return track, nil
}

func (p *anonymousBilibili) Search(ctx context.Context, request online.SearchRequest) (online.SearchPage, error) {
	track, err := p.Metadata(ctx, request.Query)
	return online.SearchPage{Kind: "tracks", Tracks: []online.Track{track}, Albums: []online.Album{}, Artists: []online.Artist{}, Playlists: []online.Playlist{}, Consistency: online.Unversioned, ObservedAt: time.Now().UTC()}, err
}

func (p *anonymousBilibili) ObservePlaylist(_ context.Context, id string) (online.PlaylistObservation, error) {
	p.calls++
	count := int64(2)
	item := online.Playlist{ExternalID: id, Title: "Public collection", SourceURL: online.ReferenceURL(onlineplugins.Describe(online.Bilibili).Resources.Playlists, id), TrackCount: &count}
	entries := []online.PlaylistEntry{{Position: 0, ExternalTrackID: "video:BV1xx411c7mD:11"}, {Position: 1, ExternalTrackID: "video:BV1xx411c7mD:12"}}
	return online.PlaylistObservation{Playlist: item, Entries: entries, Complete: true, Consistency: online.Unversioned, ObservedAt: time.Now().UTC(), MetadataDigest: online.PlaylistDigest(item, entries)}, nil
}

func (p *anonymousBilibili) MetadataBatch(ctx context.Context, ids []string) (online.TrackBatch, error) {
	items := make([]online.TrackLookup, len(ids))
	for i, id := range ids {
		track, err := p.Metadata(ctx, id)
		if err != nil {
			return online.TrackBatch{}, err
		}
		items[i] = online.TrackLookup{ExternalID: id, Track: &track}
	}
	return online.TrackBatch{Items: items}, nil
}

func (p *anonymousBilibili) PlaylistTracks(context.Context, online.PlaylistTracksRequest) (online.PlaylistTracksPage, error) {
	return online.PlaylistTracksPage{}, domain.Fail("UNSUPPORTED", "Not used by this fixture")
}

func anonymousHost(t *testing.T) (*onlinehost.Service, *anonymousBilibili) {
	t.Helper()
	p := &anonymousBilibili{}
	host, err := onlinehost.New([]onlinehost.Runtime{{Owner: "owner", InstanceID: "bili", Platform: online.Bilibili, ConfigRevision: "revision-1", Enabled: true, Provider: p}})
	if err != nil {
		t.Fatal(err)
	}
	return host, p
}

func requireBilibiliCode(t *testing.T, err error, code string) {
	t.Helper()
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

func TestAnonymousBilibiliAccessDoesNotRequirePlatformAccount(t *testing.T) {
	host, p := anonymousHost(t)
	ctx := context.Background()
	items, err := host.Instances(ctx, "owner")
	if err != nil || len(items) != 1 || items[0].Availability != "available" || items[0].AccountID != "" || items[0].AccountRevision != "" || items[0].Authentication != "anonymous" {
		t.Fatalf("anonymous instance required login: %+v %v", items, err)
	}
	track, err := host.Metadata(ctx, "owner", "bili", "audio:11")
	if err != nil || track.ExternalID != "audio:11" {
		t.Fatalf("anonymous metadata was rejected: %+v %v", track, err)
	}
	page, err := host.Search(ctx, "owner", "bili", online.SearchRequest{Query: "video:BV1xx411c7mD:11", Limit: 1})
	if err != nil || len(page.Tracks) != 1 {
		t.Fatalf("anonymous search was rejected: %+v %v", page, err)
	}
	calls := p.calls
	_, err = host.Metadata(ctx, "another-owner", "bili", "audio:11")
	requireBilibiliCode(t, err, "NOT_FOUND")
	_, err = host.Runtime(ctx, "owner", "bili", "forged-account")
	requireBilibiliCode(t, err, "ACCESS_DENIED")
	if p.calls != calls {
		t.Fatal("unauthorized request reached the provider")
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = host.Metadata(ctx, "owner", "bili", "audio:11"); !errors.Is(err, context.Canceled) || p.calls != calls {
		t.Fatal("canceled anonymous request reached the provider", err)
	}
}

func TestAnonymousImportedTracksRetainAccountBoundary(t *testing.T) {
	host, p := anonymousHost(t)
	ctx := context.Background()
	batch, err := host.MetadataBatchForAccount(ctx, "owner", "bili", "", []string{"audio:11"})
	if err != nil || len(batch.Items) != 1 || batch.Items[0].ExternalID != "audio:11" {
		t.Fatalf("anonymous imported track display failed: %+v %v", batch, err)
	}
	calls := p.calls
	_, err = host.MetadataBatchForAccount(ctx, "owner", "bili", "forged-account", []string{"audio:11"})
	requireBilibiliCode(t, err, "ACCESS_DENIED")
	_, err = host.MetadataBatchForAccount(ctx, "another-owner", "bili", "", []string{"audio:11"})
	requireBilibiliCode(t, err, "NOT_FOUND")
	if p.calls != calls {
		t.Fatal("account or owner spoofing reached display metadata")
	}
}

type bilibiliImportRepository struct {
	playlists.Repository
	bindings []playlists.Binding
}

func (r *bilibiliImportRepository) ImportRemotePlaylist(_ context.Context, owner string, binding playlists.Binding, _ string) (playlists.RemotePlaylist, bool, error) {
	r.bindings = append(r.bindings, binding)
	return playlists.RemotePlaylist{ID: "imported", Owner: owner, RemoteRef: binding.RemoteRef, Platform: binding.Platform, ConfigRevision: binding.ConfigRevision}, false, nil
}

func TestAnonymousBilibiliFavoriteImportPreservesNamespaceAndBinding(t *testing.T) {
	host, p := anonymousHost(t)
	repository := &bilibiliImportRepository{}
	service, err := playlists.New(repository, host.PlaylistRemote(), 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, id := range []string{"favorite:91", "audio-menu:91"} {
		ref := playlists.RemoteRef{InstanceID: "bili", ExternalID: id}
		item, _, err := service.Import(ctx, "owner", ref, "import-"+id)
		if err != nil || item.RemoteRef != ref || item.Platform != online.Bilibili || item.ConfigRevision != "revision-1" || p.calls != 0 {
			t.Fatalf("import changed anonymous scope or fetched content: %+v %v", item, err)
		}
	}
	if len(repository.bindings) != 2 || repository.bindings[0].ExternalID == repository.bindings[1].ExternalID {
		t.Fatal("favorite and audio menu identities collided")
	}
	for _, tc := range []struct{ owner, account, external, code string }{
		{"another-owner", "", "favorite:91", "NOT_FOUND"},
		{"owner", "forged-account", "favorite:91", "ACCESS_DENIED"},
		{"owner", "", "91", "INVALID_ARGUMENT"},
		{"owner", "", "audio:91", "INVALID_ARGUMENT"},
		{"owner", "", "video:BV1xx411c7mD:11", "INVALID_ARGUMENT"},
		{"owner", "", "https://www.bilibili.com/medialist/detail/ml91", "INVALID_ARGUMENT"},
	} {
		_, _, err := service.Import(ctx, tc.owner, playlists.RemoteRef{InstanceID: "bili", AccountID: tc.account, ExternalID: tc.external}, "rejected")
		requireBilibiliCode(t, err, tc.code)
	}
	if len(repository.bindings) != 2 || p.calls != 0 {
		t.Fatal("invalid import wrote a binding or reached the provider")
	}
	remote := host.PlaylistRemote()
	binding := repository.bindings[0]
	_, release, err := remote.(interface {
		Acquire(context.Context, string, playlists.Binding) (context.Context, func(), error)
	}).Acquire(ctx, "owner", binding)
	if err != nil {
		t.Fatal("anonymous binding lease failed", err)
	}
	release()
	observation, err := remote.ObservePlaylist(ctx, "owner", binding)
	if err != nil || observation.Validate("favorite:91") != nil || p.calls != 1 {
		t.Fatal("anonymous collection observation failed", err)
	}
	binding.ConfigRevision = "old-revision"
	_, err = remote.ObservePlaylist(ctx, "owner", binding)
	requireBilibiliCode(t, err, "RESOURCE_CHANGED")
	if p.calls != 1 {
		t.Fatal("stale collection binding reached the provider")
	}
}
