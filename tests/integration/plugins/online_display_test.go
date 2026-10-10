package plugins_test

import (
	"context"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/online"
)

func TestActualOnlineWorkerEntityAndBatchProtocol(t *testing.T) {
	path := onlineBinary(t, "./tests/testkit/online-worker", "online-display")
	w := onlineStart(t, onlineConfig(t, path, online.Netease))
	loginOnlineFixture(t, w)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	for _, kind := range []string{"albums", "artists", "playlists"} {
		p, err := w.Search(ctx, online.SearchRequest{Kind: kind, Query: "Fixture", Limit: 1})
		if err != nil || p.Kind != kind || len(p.Albums)+len(p.Artists)+len(p.Playlists) != 1 {
			t.Fatal(kind, p, err)
		}
		time.Sleep(1050 * time.Millisecond)
	}
	a, err := w.Album(ctx, "91")
	if err != nil || a.ExternalID != "91" {
		t.Fatal(a, err)
	}
	time.Sleep(1050 * time.Millisecond)
	artist, err := w.Artist(ctx, "92")
	if err != nil || artist.ExternalID != "92" {
		t.Fatal(artist, err)
	}
	for _, tc := range []struct {
		kind, id string
		load     func(context.Context, online.EntityPageRequest) (online.SearchPage, error)
	}{{"tracks", "91", w.AlbumTracks}, {"albums", "92", w.ArtistAlbums}, {"tracks", "92", w.ArtistTracks}} {
		time.Sleep(1050 * time.Millisecond)
		page, err := tc.load(ctx, online.EntityPageRequest{ExternalID: tc.id, Limit: 1})
		if err != nil || page.Kind != tc.kind {
			t.Fatal(tc.id, page, err)
		}
	}
	time.Sleep(1050 * time.Millisecond)
	batch, err := w.MetadataBatch(ctx, []string{"42", "43", "42"})
	if err != nil || len(batch.Items) != 3 || batch.Items[1].ErrorCode == nil {
		t.Fatal(batch, err)
	}
	time.Sleep(1050 * time.Millisecond)
	page, err := w.PlaylistTracks(ctx, online.PlaylistTracksRequest{ExternalID: "91", Limit: 2})
	if err != nil || page.NextCursor == nil || len(page.Items) != 2 || page.Items[1].Position != 1 {
		t.Fatal(page, err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
}
