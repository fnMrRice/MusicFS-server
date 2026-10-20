package online_test

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	online "github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/adapter"
)

func TestEntitySearchSeparatesKindsAndBindsCursors(t *testing.T) {
	for _, tc := range []struct{ kind, body string }{
		{"albums", `{"code":200,"result":{"albumCount":2,"albums":[{"id":91,"name":"Album","artists":[{"name":"Artist"}],"size":2,"picUrl":"http://p1.music.126.net/x/y.jpg"}]}}`},
		{"artists", `{"code":200,"result":{"artistCount":2,"artists":[{"id":91,"name":"Artist","albumSize":2,"musicSize":3,"picUrl":"https://p2.music.126.net/x/y.jpg"}]}}`},
		{"playlists", `{"code":200,"result":{"playlistCount":2,"playlists":[{"id":91,"name":"Playlist","trackCount":2,"creator":{"nickname":"Creator"},"coverImgUrl":"http://p1.music.126.net/x/y.jpg"}]}}`},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			var calls atomic.Int32
			c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if r.URL.Query().Get("type") == "1" {
					t.Fatal("entity search used track endpoint")
				}
				return response(tc.body).RoundTrip(r)
			}))
			request := online.SearchRequest{Kind: tc.kind, Query: "Name", Limit: 1}
			page, err := c.Search(context.Background(), request)
			if err != nil || page.Kind != tc.kind || page.NextCursor == nil || len(page.Tracks) != 0 {
				t.Fatal("entity page", page, err)
			}
			if err = online.ValidateSearch(online.Netease, page, 1); err != nil {
				t.Fatal(err)
			}
			request.Cursor = *page.NextCursor
			request.Kind = "tracks"
			_, err = c.Search(context.Background(), request)
			expectCode(t, err, "INVALID_ARGUMENT")
			if calls.Load() != 1 {
				t.Fatal("cross-kind cursor reached upstream")
			}
		})
	}
	for _, kind := range []string{"albums", "artists", "playlists"} {
		qq := scoped(t, online.QQMusic, response(`{}`))
		_, err := qq.Search(context.Background(), online.SearchRequest{Kind: kind, Query: "name", Limit: 1})
		expectCode(t, err, "UNSUPPORTED")
	}
}

func TestTrackBatchPreservesDuplicatesAndMissingWithoutNPlusOne(t *testing.T) {
	var calls atomic.Int32
	c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Query().Get("c") != `[{"id":42},{"id":43}]` {
			t.Fatal("batch not deduplicated", r.URL)
		}
		return response(`{"code":200,"songs":[{"id":42,"name":"Track","ar":[],"al":{},"dt":123}]}`).RoundTrip(r)
	}))
	ids := []string{"42", "43", "42"}
	batch, err := c.MetadataBatch(context.Background(), ids)
	if err != nil || calls.Load() != 1 || len(batch.Items) != 3 || batch.Items[0].Track == nil || batch.Items[2].Track == nil || batch.Items[1].ErrorCode == nil || *batch.Items[1].ErrorCode != "NOT_FOUND" {
		t.Fatal(batch, err)
	}
	if err = online.ValidateTrackBatch(online.Netease, batch, ids); err != nil {
		t.Fatal(err)
	}
	batch.Items[0].Track.PlaybackAvailability = "available"
	if online.ValidateTrackBatch(online.Netease, batch, ids) == nil {
		t.Fatal("invented playback permission accepted")
	}
}

func TestPlaylistTracksRejectsChangedObservationAndCancelsBatch(t *testing.T) {
	var observations atomic.Int32
	c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "playlist") {
			ids := `[{"id":42},{"id":43}]`
			if observations.Add(1) > 1 {
				ids = `[{"id":44},{"id":43}]`
			}
			return response(`{"code":200,"playlist":{"id":91,"name":"Playlist","privacy":0,"trackCount":2,"trackIds":` + ids + `}}`).RoundTrip(r)
		}
		return response(`{"code":200,"songs":[{"id":42,"name":"Track","ar":[],"al":{},"dt":123}]}`).RoundTrip(r)
	}))
	r := online.PlaylistTracksRequest{ExternalID: "91", Limit: 1}
	page, err := c.PlaylistTracks(context.Background(), r)
	if err != nil || page.NextCursor == nil || len(page.Items) != 1 || page.Items[0].Position != 0 {
		t.Fatal(page, err)
	}
	if err = online.ValidatePlaylistTracks(online.Netease, page, r); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1050 * time.Millisecond)
	r.Cursor = *page.NextCursor
	_, err = c.PlaylistTracks(context.Background(), r)
	expectCode(t, err, "CURSOR_EXPIRED")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = c.PlaylistTracks(ctx, online.PlaylistTracksRequest{ExternalID: "91", Limit: 1}); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}

func TestEntitySearchRejectsUnsafeImageOrMalformedMetadata(t *testing.T) {
	for _, body := range []string{
		`{"code":200,"result":{"albumCount":1,"albums":[{"id":91,"name":"Album","picUrl":"https://127.0.0.1/private"}]}}`,
		`{"code":200,"result":{"albumCount":1,"albums":[{"id":91,"name":"Album","size":-1}]}}`,
		`{"code":200,"result":{"albums":[]}}`,
	} {
		c := scoped(t, online.Netease, response(body))
		_, err := c.Search(context.Background(), online.SearchRequest{Kind: "albums", Query: "test", Limit: 1})
		expectCode(t, err, "TEMPORARY_FAILURE")
	}
}
