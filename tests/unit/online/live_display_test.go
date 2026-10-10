package online_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/online"
)

func TestLiveAnonymousSearchAndBatchedDisplay(t *testing.T) {
	if os.Getenv("MUSICFS_ONLINE_LIVE") != "1" {
		t.Skip("explicit public-network validation not enabled")
	}
	c, err := online.New(online.Netease, online.Scope{UserID: "validation", InstanceID: "netease-public"}, online.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var tracks []string
	var playlist string
	for _, kind := range []string{"tracks", "albums", "artists", "playlists"} {
		page, err := c.Search(ctx, online.SearchRequest{Kind: kind, Query: "Daft Punk", Limit: 2})
		if err != nil {
			t.Fatal(kind, err)
		}
		if err = online.ValidateSearch(online.Netease, page, 2); err != nil {
			t.Fatal(kind, err)
		}
		count := len(page.Tracks) + len(page.Albums) + len(page.Artists) + len(page.Playlists)
		if count == 0 {
			t.Fatal("live entity search returned no sample", kind)
		}
		if kind == "tracks" {
			for _, track := range page.Tracks {
				tracks = append(tracks, track.ExternalID)
			}
		}
		if kind == "playlists" {
			playlist = page.Playlists[0].ExternalID
		}
		t.Logf("kind=%s items=%d next=%t", kind, count, page.NextCursor != nil)
		time.Sleep(1100 * time.Millisecond)
	}
	batch, err := c.MetadataBatch(ctx, tracks)
	if err != nil {
		t.Fatal(err)
	}
	if err = online.ValidateTrackBatch(online.Netease, batch, tracks); err != nil {
		t.Fatal(err)
	}
	for _, item := range batch.Items {
		if item.Track == nil {
			t.Fatal("live batch item missing", item.ExternalID)
		}
	}
	time.Sleep(1100 * time.Millisecond)
	request := online.PlaylistTracksRequest{ExternalID: playlist, Limit: 2}
	page, err := c.PlaylistTracks(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if err = online.ValidatePlaylistTracks(online.Netease, page, request); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) == 0 {
		t.Fatal("live playlist page empty")
	}
	t.Logf("batch=%d playlistItems=%d complete=%t next=%t", len(batch.Items), len(page.Items), page.Complete, page.NextCursor != nil)
}

func TestLiveAnonymousEntityDetailsAndRelationshipPaging(t *testing.T) {
	if os.Getenv("MUSICFS_ONLINE_LIVE") != "1" {
		t.Skip("explicit public-network validation not enabled")
	}
	c, err := online.New(online.Netease, online.Scope{UserID: "validation", InstanceID: "netease-public"}, online.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	a, err := c.Album(ctx, "2462086")
	if err != nil || online.ValidateAlbum(online.Netease, a) != nil {
		t.Fatal("album detail", a, err)
	}
	time.Sleep(1100 * time.Millisecond)
	artist, err := c.Artist(ctx, "90513")
	if err != nil || online.ValidateArtist(online.Netease, artist) != nil {
		t.Fatal("artist detail", artist, err)
	}
	for _, tc := range []struct {
		name, id string
		load     func(context.Context, online.EntityPageRequest) (online.SearchPage, error)
	}{{"albumTracks", "2462086", c.AlbumTracks}, {"artistAlbums", "90513", c.ArtistAlbums}, {"artistTracks", "90513", c.ArtistTracks}} {
		time.Sleep(1100 * time.Millisecond)
		r := online.EntityPageRequest{ExternalID: tc.id, Limit: 2}
		first, err := tc.load(ctx, r)
		if err != nil || first.NextCursor == nil || online.ValidateSearch(online.Netease, first, 2) != nil {
			t.Fatal(tc.name, first, err)
		}
		time.Sleep(1100 * time.Millisecond)
		r.Cursor = *first.NextCursor
		second, err := tc.load(ctx, r)
		if err != nil || online.ValidateSearch(online.Netease, second, 2) != nil {
			t.Fatal(tc.name, second, err)
		}
		if len(first.Tracks) > 0 && first.Tracks[0].ExternalID == second.Tracks[0].ExternalID {
			t.Fatal("track page repeated", tc.name)
		}
		if len(first.Albums) > 0 && first.Albums[0].ExternalID == second.Albums[0].ExternalID {
			t.Fatal("album page repeated", tc.name)
		}
		t.Logf("relationship=%s first=%d next=%d consistency=%s", tc.name, len(first.Tracks)+len(first.Albums), len(second.Tracks)+len(second.Albums), second.Consistency)
	}
}
