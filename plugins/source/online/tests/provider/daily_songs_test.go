package provideronline_test

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestWorkerDailyPlaylistUsesAccountAndRetainsMetadataAcrossPages(t *testing.T) {
	t.Parallel()
	const payload = `{"code":200,"data":{"dailySongs":[
		{"id":9007199254740993,"name":"第一首","ar":[{"name":"甲"},{"name":"乙"}],"al":{"name":"专辑"},"dt":180001},
		{"id":52,"name":"第二首","ar":[]},{"id":12,"name":"第三首","ar":[]}]}}`
	var changed atomic.Bool
	peer := worker(t, transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v3/discovery/recommend/songs" || r.URL.RawQuery != "" {
			t.Errorf("unexpected daily songs request: %s", r.URL)
		}
		if _, err := r.Cookie("MUSIC_U"); err != nil {
			t.Error("daily songs request omitted the platform session")
		}
		body := payload
		if changed.Load() {
			body = strings.Replace(body, `"id":12`, `"id":52`, 1)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	hello(t, peer)
	ctx := context.Background()
	request := pluginproto.OnlineRecommendations{Context: scope(), Limit: 1}
	assertCode(t, peer.Call(ctx, "provider.online.recommendations", request, nil), "PLATFORM_AUTH_REQUIRED")
	authenticate(t, peer)
	assertCode(t, peer.Call(ctx, "provider.online.dailySongs", request, nil), "UNSUPPORTED")
	for _, limit := range []int{0, online.MaxPageItems + 1} {
		invalid := pluginproto.OnlineRecommendations{Context: scope(), Limit: limit}
		assertCode(t, peer.Call(ctx, "provider.online.recommendations", invalid, nil), "INVALID_ARGUMENT")
	}
	var recs pluginproto.OnlineResult[online.RecommendationPage]
	if err := peer.Call(ctx, "provider.online.recommendations", request, &recs); err != nil || len(recs.Value.Items) != 1 || !recs.Value.Personalized {
		t.Fatal("daily playlist missing", recs, err)
	}
	policy := onlineplugins.Describe(online.Netease).Resources
	if err := online.ValidateRecommendations(policy, recs.Value, request.Limit); err != nil {
		t.Fatal(err)
	}
	id := recs.Value.Items[0].Playlist.ExternalID
	observe := pluginproto.OnlineReference{Context: scope(), ExternalID: id}
	var observed pluginproto.OnlineResult[online.PlaylistObservation]
	if err := peer.Call(ctx, "provider.online.playlist", observe, &observed); err != nil || online.ValidateObservation(policy, observed.Value, id) != nil || !observed.Value.Complete || len(observed.Value.Entries) != 3 {
		t.Fatal("invalid observation", observed, err)
	}
	observe.ExternalID = "daily-songs:678"
	assertCode(t, peer.Call(ctx, "provider.online.playlist", observe, nil), "NOT_FOUND")
	write := pluginproto.OnlinePlaylistTrackAddition{Context: scope(), PlaylistTrackAddition: online.PlaylistTrackAddition{AccountID: "12345", ExternalID: id, TrackID: "12"}}
	assertCode(t, peer.Call(ctx, "provider.online.addPlaylistTrack", write, nil), "ACCESS_DENIED")
	page := pluginproto.OnlinePlaylistTracks{Context: scope(), PlaylistTracksRequest: online.PlaylistTracksRequest{ExternalID: id, Limit: 2}}
	var result pluginproto.OnlineResult[online.PlaylistTracksPage]
	if err := peer.Call(ctx, "provider.online.playlistTracks", page, &result); err != nil || online.ValidatePlaylistTracks(policy, result.Value, page.PlaylistTracksRequest) != nil || result.Value.NextCursor == nil || len(result.Value.Items) != 2 {
		t.Fatal("invalid first page", result, err)
	}
	track := result.Value.Items[0].Track
	if track == nil || track.ExternalID != "9007199254740993" || track.Title != "第一首" || !slices.Equal(track.Artists, []string{"甲", "乙"}) || track.Album != "专辑" || track.DurationMs == nil || *track.DurationMs != 180001 || result.Value.Items[1].ExternalID != "52" {
		t.Fatal("daily song metadata lost", result.Value.Items)
	}
	page.Cursor = *result.Value.NextCursor
	if err := peer.Call(ctx, "provider.online.playlistTracks", page, &result); err != nil || len(result.Value.Items) != 1 || result.Value.Items[0].ExternalID != "12" || result.Value.Items[0].Position != 2 || result.Value.NextCursor != nil {
		t.Fatal("invalid continuation", result, err)
	}
	changed.Store(true)
	assertCode(t, peer.Call(ctx, "provider.online.playlistTracks", page, nil), "CURSOR_EXPIRED")
}
