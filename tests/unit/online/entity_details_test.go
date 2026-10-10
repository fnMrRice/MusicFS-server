package online_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/online"
)

func TestEntityDetailsAndRelationshipsValidateIdentityAndCursors(t *testing.T) {
	for _, tc := range []struct {
		path, body string
		run        func(*online.Client) error
	}{
		{"/api/v1/album/91", `{"code":200,"album":{"id":91,"name":"Album","size":1},"songs":[{"id":42,"name":"Track","ar":[],"al":{},"dt":123}]}`, func(c *online.Client) error {
			a, e := c.Album(context.Background(), "91")
			if e == nil {
				e = online.ValidateAlbum(online.Netease, a)
			}
			return e
		}},
		{"/api/artist/92", `{"code":200,"artist":{"id":92,"name":"Artist"}}`, func(c *online.Client) error {
			a, e := c.Artist(context.Background(), "92")
			if e == nil {
				e = online.ValidateArtist(online.Netease, a)
			}
			return e
		}},
		{"/api/artist/albums/92", `{"code":200,"artist":{"id":92,"name":"Artist"},"hotAlbums":[{"id":91,"name":"Album"}],"more":true}`, func(c *online.Client) error {
			p, e := c.ArtistAlbums(context.Background(), online.EntityPageRequest{ExternalID: "92", Limit: 1})
			if e == nil {
				e = online.ValidateSearch(online.Netease, p, 1)
				if p.NextCursor == nil {
					t.Fatal("album cursor missing")
				}
			}
			return e
		}},
		{"/api/v1/artist/songs", `{"code":200,"songs":[{"id":42,"name":"Track","artists":[],"album":{},"duration":123,"mp3Url":"https://private.invalid/audio"}],"more":true,"total":2}`, func(c *online.Client) error {
			p, e := c.ArtistTracks(context.Background(), online.EntityPageRequest{ExternalID: "92", Limit: 1})
			if e == nil {
				e = online.ValidateSearch(online.Netease, p, 1)
				if p.NextCursor == nil {
					t.Fatal("track cursor missing")
				}
			}
			return e
		}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != tc.path {
					t.Fatal(r.URL)
				}
				return response(tc.body).RoundTrip(r)
			}))
			if err := tc.run(c); err != nil {
				t.Fatal(err)
			}
		})
	}
	c := scoped(t, online.Netease, response(`{"code":200,"artist":{"id":93,"name":"Wrong"}}`))
	_, err := c.Artist(context.Background(), "92")
	expectCode(t, err, "TEMPORARY_FAILURE")
}

func TestAlbumPagingBindsObservationAndRefusesCrossRelationshipCursor(t *testing.T) {
	var calls atomic.Int32
	c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
		name := "First"
		if calls.Add(1) > 1 {
			name = "Changed"
		}
		return response(`{"code":200,"album":{"id":91,"name":"Album","size":2},"songs":[{"id":42,"name":"` + name + `","ar":[],"al":{},"dt":123},{"id":43,"name":"Next","ar":[],"al":{},"dt":123}]}`).RoundTrip(r)
	}))
	r := online.EntityPageRequest{ExternalID: "91", Limit: 1}
	p, err := c.AlbumTracks(context.Background(), r)
	if err != nil || p.NextCursor == nil || p.Truncated {
		t.Fatal(p, err)
	}
	r.Cursor = *p.NextCursor
	_, err = c.ArtistTracks(context.Background(), r)
	expectCode(t, err, "INVALID_ARGUMENT")
	if calls.Load() != 1 {
		t.Fatal("cross-relationship cursor reached upstream")
	}
	time.Sleep(1050 * time.Millisecond)
	_, err = c.AlbumTracks(context.Background(), r)
	expectCode(t, err, "CURSOR_EXPIRED")
}
