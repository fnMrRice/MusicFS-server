package bilibili

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

func TestFavoritesFullPagingPartsDuplicatesAndUnavailable(t *testing.T) {
	viewCalls, listCalls := 0, 0
	c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/x/web-interface/view" {
			viewCalls++
			video := videoFixture()
			video["pic"] = "http://i0.hdslb.com/video.jpg"
			video["pages"].([]any)[0].(map[string]any)["first_frame"] = "//i0.hdslb.com/part.jpg"
			return response(t, video), nil
		}
		listCalls++
		media := []any{}
		if r.URL.Query().Get("pn") == "1" {
			for i := 0; i < 20; i++ {
				media = append(media, map[string]any{"id": 8903802, "bvid": "BV13x41117TL", "type": 2})
			}
		} else {
			media = append(media, map[string]any{"id": 500, "type": 2})
		}
		return response(t, map[string]any{"info": map[string]any{"id": 99, "title": "Favorites", "media_count": 21, "upper": map[string]any{"name": "Owner"}}, "medias": media, "has_more": r.URL.Query().Get("pn") == "1"}), nil
	})
	r := online.PlaylistTracksRequest{ExternalID: "favorite:99", Limit: 30}
	page, err := c.PlaylistTracks(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if !page.Complete || *page.Playlist.TrackCount != 41 || len(page.Items) != 30 || page.NextCursor == nil || viewCalls != 1 || listCalls != 2 {
		t.Fatalf("incomplete favorites observation: %+v", page)
	}
	if err := online.ValidatePlaylistTracks(Plugin(Options{}).Descriptor.Resources, page, r); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"https://i0.hdslb.com/part.jpg", "https://i0.hdslb.com/video.jpg"} {
		if got := page.Items[i].Track.CoverURL; got == nil || *got != want {
			t.Fatalf("wrong image for video part %d", i+1)
		}
	}
	r.Cursor = *page.NextCursor
	last, err := c.PlaylistTracks(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(last.Items) != 11 || last.Items[10].ExternalID != "unavailable:2:500" || last.Items[10].ErrorCode == nil || last.MetadataDigest != page.MetadataDigest || listCalls != 2 {
		t.Fatal("favorites paging lost its snapshot or missing item")
	}
	r.ExternalID = "favorite:100"
	_, err = c.PlaylistTracks(context.Background(), r)
	errorCode(t, err, "INVALID_ARGUMENT")
	observation := c.snapshots[strings.Split(r.Cursor, ":")[0]].observation
	if err := online.ValidateObservation(Plugin(Options{}).Descriptor.Resources, observation, "favorite:99"); err != nil {
		t.Fatal(err)
	}
}

func TestAudioMenuReadsEveryPageAndPreservesDuplicates(t *testing.T) {
	pages := 0
	c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/menu/info") {
			return response(t, map[string]any{"menuId": 88, "title": "Audio menu", "uname": "Owner", "snum": 0}), nil
		}
		pages++
		pn, _ := strconv.Atoi(r.URL.Query().Get("pn"))
		n := 100
		if pn == 2 {
			n = 1
		}
		songs := []any{}
		for i := 0; i < n; i++ {
			songs = append(songs, map[string]any{"id": 10, "title": "Audio", "duration": 60})
		}
		return response(t, map[string]any{"curPage": pn, "pageCount": 2, "totalSize": 101, "data": songs}), nil
	})
	observation, err := c.ObservePlaylist(context.Background(), "audio-menu:88")
	if err != nil {
		t.Fatal(err)
	}
	if pages != 2 || !observation.Complete || len(observation.Entries) != 101 || *observation.Playlist.TrackCount != 101 || observation.Entries[100].Position != 100 {
		t.Fatal("audio menu pagination lost duplicate entries")
	}
	if err := online.ValidateObservation(Plugin(Options{}).Descriptor.Resources, observation, "audio-menu:88"); err != nil {
		t.Fatal(err)
	}
}

func TestCollectionPartialFailureIsNotPublishedAsEmpty(t *testing.T) {
	c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/menu/info") {
			return response(t, map[string]any{"menuId": 88, "title": "Audio menu"}), nil
		}
		if r.URL.Query().Get("pn") == "2" {
			return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"7"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return response(t, map[string]any{"curPage": 1, "pageCount": 2, "totalSize": 2, "data": []any{map[string]any{"id": 10, "title": "Audio"}}}), nil
	})
	_, err := c.PlaylistTracks(context.Background(), online.PlaylistTracksRequest{ExternalID: "audio-menu:88", Limit: 50})
	errorCode(t, err, "RATE_LIMITED")
	if len(c.snapshots) != 0 {
		t.Fatal("partial failure published a cached snapshot")
	}
}

func TestEmptyCollectionAndChangingCount(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		c := newTestClient(t, func(*http.Request) (*http.Response, error) {
			return response(t, map[string]any{"info": map[string]any{"id": 99, "title": "Empty", "media_count": 0}, "medias": nil, "has_more": false}), nil
		})
		o, err := c.ObservePlaylist(context.Background(), "favorite:99")
		if err != nil {
			t.Fatal(err)
		}
		if !o.Complete || o.Entries == nil || len(o.Entries) != 0 {
			t.Fatal("empty collection was not represented correctly")
		}
	})
	t.Run("changing", func(t *testing.T) {
		c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
			pn, _ := strconv.Atoi(r.URL.Query().Get("pn"))
			return response(t, map[string]any{"info": map[string]any{"id": 99, "title": "Changed", "media_count": pn + 1}, "medias": []any{map[string]any{"id": 1, "type": 99}}, "has_more": true}), nil
		})
		_, err := c.ObservePlaylist(context.Background(), "favorite:99")
		errorCode(t, err, "TEMPORARY_FAILURE")
	})
}

func TestCollectionLookupDoesNotExpandItemsAndRejectsPrivate(t *testing.T) {
	for _, private := range []bool{false, true} {
		calls := 0
		c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
			calls++
			attr := 22
			if private {
				attr = 23
			}
			if r.URL.Path != "/x/v3/fav/resource/list" || r.URL.Query().Get("pn") != "1" {
				t.Fatal("collection lookup expanded tracks")
			}
			return response(t, map[string]any{"info": map[string]any{"id": 99, "title": "Favorites", "attr": attr, "media_count": 1000}, "medias": []any{map[string]any{"id": 8903802, "type": 2, "bvid": "BV13x41117TL"}}, "has_more": true}), nil
		})
		page, err := c.Search(context.Background(), online.SearchRequest{Kind: "playlists", Query: "favorite:99", Limit: 10})
		if private {
			errorCode(t, err, "ACCESS_DENIED")
		} else {
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Playlists) != 1 || page.Playlists[0].TrackCount != nil {
				t.Fatal("collection lookup invented an expanded track count")
			}
		}
		if calls != 1 {
			t.Fatal("collection metadata lookup made more than one request")
		}
	}
}

func TestFavoritesCapReturnsIncompleteUnknownCount(t *testing.T) {
	c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/x/web-interface/view" {
			info := videoFixture()
			parts := []any{}
			for i := 1; i <= online.MaxPlaylistEntries; i++ {
				parts = append(parts, map[string]any{"cid": i, "page": i, "part": "Part", "duration": 1})
			}
			info["pages"] = parts
			return response(t, info), nil
		}
		return response(t, map[string]any{"info": map[string]any{"id": 99, "title": "Large", "media_count": 2}, "medias": []any{map[string]any{"id": 8903802, "type": 2, "bvid": "BV13x41117TL"}, map[string]any{"id": 8903802, "type": 2, "bvid": "BV13x41117TL"}}, "has_more": false}), nil
	})
	o, err := c.ObservePlaylist(context.Background(), "favorite:99")
	if err != nil {
		t.Fatal(err)
	}
	if o.Complete || o.Playlist.TrackCount != nil || len(o.Entries) != online.MaxPlaylistEntries {
		t.Fatal("truncated collection claimed a complete count")
	}
	if err := online.ValidateObservation(Plugin(Options{}).Descriptor.Resources, o, "favorite:99"); err != nil {
		t.Fatal(err)
	}
}
