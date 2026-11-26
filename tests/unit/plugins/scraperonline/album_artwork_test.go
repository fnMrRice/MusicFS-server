package scraperonline_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/scraper/online"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
)

type artworkAlbums struct {
	music.Repository
	ref    string
	fields []works.ScrapedField
}

func (r *artworkAlbums) MusicAlbum(_ context.Context, owner, id string) (music.Album, error) {
	if owner != "alice" || id != "album" {
		return music.Album{}, errors.New("unknown album")
	}
	return music.Album{ID: id, AlbumFields: music.AlbumFields{CoverPath: &r.ref}}, nil
}

func (r *artworkAlbums) MusicAlbumScrapedFields(context.Context, string, string) ([]works.ScrapedField, error) {
	if r.fields != nil {
		return r.fields, nil
	}
	return []works.ScrapedField{{Field: "albumDetails", PluginID: "musicfs.scraper.netease", Item: scraper.Item{ID: "22", ImageURL: r.ref}}}, nil
}

type artworkCDN struct {
	calls   atomic.Int32
	lookups atomic.Int32
	bytes   []byte
}

func (p *artworkCDN) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host == "music.163.com" && r.URL.Path == "/api/v1/album/22" {
		p.lookups.Add(1)
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"code":200,"album":{"id":22,"name":"Album","size":1,"picUrl":"https://p1.music.126.net/new.png"},"songs":[{"id":1,"name":"One","dt":1000,"cd":"1","no":1}]}`))}, nil
	}
	if p.lookups.Load() > 0 && r.URL.Path != "/new.png" {
		return nil, errors.New("stale cover URL")
	}
	if r.URL.Host != "p1.music.126.net" || r.Header.Get("Authorization") != "" {
		return nil, errors.New("unexpected upstream request")
	}
	p.calls.Add(1)
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(p.bytes)), Header: http.Header{"Content-Type": {"image/png"}}}, nil
}

func coverImage(t *testing.T, value uint8) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 3, 3))
	img.SetGray(0, 0, color.Gray{Y: value})
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestAlbumArtworkPersistsUntilExplicitRefresh(t *testing.T) {
	ctx := t.Context()
	for _, ref := range []string{"https://p1.music.126.net/one.png", "/api/v1/source-tracks/one/online-artwork"} {
		t.Run(ref, func(t *testing.T) {
			repo := &artworkAlbums{ref: ref}
			first, second := coverImage(t, 40), coverImage(t, 180)
			plugin := &artworkCDN{bytes: first}
			service, err := onlinescrape.New(online.Catalog(&http.Client{Transport: plugin}, ""))
			if err != nil {
				t.Fatal(err)
			}
			var sourceCalls atomic.Int32
			resolve := func(_ context.Context, owner, source string, refresh bool) (string, error) {
				if owner != "alice" || source != "one" {
					return "", errors.New("wrong source")
				}
				if n := sourceCalls.Add(1); (n > 1) != refresh {
					t.Errorf("unexpected refresh flag on lookup %d", n)
				}
				return "https://p1.music.126.net/one.png", nil
			}
			dir := t.TempDir()
			cache := onlinescrape.NewAlbumArtwork(dir, service, repo, resolve)
			var wg sync.WaitGroup
			for range 6 {
				wg.Go(func() {
					b, mime, err := cache.Load(ctx, "alice", "album", false)
					if err != nil || mime != "image/png" || !bytes.Equal(b, first) {
						t.Errorf("initial read: %s %v", mime, err)
					}
				})
			}
			wg.Wait()
			if plugin.calls.Load() != 1 {
				t.Fatalf("unexpected image download count: %d", plugin.calls.Load())
			}
			if _, _, err := cache.Load(ctx, "bob", "album", false); err == nil {
				t.Fatal("another user could read the cached album")
			}
			if ref[0] != '/' {
				repo.ref = "https://p1.music.126.net/revised.png"
			}
			cache = onlinescrape.NewAlbumArtwork(dir, service, repo, resolve)
			plugin.bytes = []byte("invalid image")
			b, _, err := cache.Load(ctx, "alice", "album", false)
			if err != nil || !bytes.Equal(b, first) || plugin.calls.Load() != 1 {
				t.Fatal("reopening the library contacted the plugin", err)
			}
			if _, _, err = cache.Load(ctx, "alice", "album", true); err == nil {
				t.Fatal("invalid artwork replaced the cache")
			}
			b, _, err = cache.Load(ctx, "alice", "album", false)
			if err != nil || !bytes.Equal(b, first) {
				t.Fatal("failed update lost previous artwork", err)
			}
			plugin.bytes = second
			b, _, err = cache.Load(ctx, "alice", "album", true)
			if err != nil || !bytes.Equal(b, second) {
				t.Fatal("explicit update did not publish new image", err)
			}
			b, _, err = cache.Load(ctx, "alice", "album", false)
			if err != nil || !bytes.Equal(b, second) || plugin.calls.Load() != 3 {
				t.Fatal("updated cache was not reused", err)
			}
			if ref[0] != '/' && plugin.lookups.Load() != 2 {
				t.Fatalf("unexpected album metadata requests: %d", plugin.lookups.Load())
			}
			if ref[0] == '/' && sourceCalls.Load() != 3 {
				t.Fatalf("unexpected upstream metadata requests: %d", sourceCalls.Load())
			}
		})
	}
}

func TestMergedAlbumArtworkUsesThePluginThatOwnsTheImage(t *testing.T) {
	const ref = "https://p1.music.126.net/original.png"
	repo := &artworkAlbums{ref: ref, fields: []works.ScrapedField{
		{Field: "albumDetails", PluginID: "musicfs.scraper.musicbrainz", Item: scraper.Item{ID: "unrelated-release", ImageURL: ref}},
		{Field: "albumTracks", PluginID: "musicfs.scraper.netease", Item: scraper.Item{ID: "22", ImageURL: ref}},
	}}
	cdn := &artworkCDN{bytes: coverImage(t, 1)}
	service, err := onlinescrape.New(online.Catalog(&http.Client{Transport: cdn}, ""))
	if err != nil {
		t.Fatal(err)
	}
	cache := onlinescrape.NewAlbumArtwork(t.TempDir(), service, repo, nil)
	for _, refresh := range []bool{false, true, true, false} {
		data, _, err := cache.Load(t.Context(), "alice", "album", refresh)
		if err != nil || !bytes.Equal(data, cdn.bytes) {
			t.Fatalf("load merged cover (refresh=%v): %v", refresh, err)
		}
	}
	if cdn.lookups.Load() != 2 || cdn.calls.Load() != 3 {
		t.Fatalf("unexpected release queries or downloads: %d, %d", cdn.lookups.Load(), cdn.calls.Load())
	}
}
