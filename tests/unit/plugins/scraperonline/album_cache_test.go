package scraperonline_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginsettings"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

type albumCachePlugin struct {
	id     string
	search func(context.Context, string, scraper.Query) ([]scraper.Item, error)
}

func (p albumCachePlugin) Descriptor() scraper.Descriptor {
	return scraper.Descriptor{PluginID: p.id, Type: "scraper", Name: p.id, Available: true, Fields: []string{"albumDetails", "albumTracks", "trackDetails"}}
}

func (p albumCachePlugin) Search(ctx context.Context, field string, q scraper.Query) ([]scraper.Item, error) {
	return p.search(ctx, field, q)
}

func (p albumCachePlugin) SettingsSchema() pluginsettings.Schema { return pluginsettings.Schema{} }
func (p albumCachePlugin) ValidateSettings(map[string]any) error { return nil }
func (p albumCachePlugin) WithSettings(map[string]any) (scraper.Plugin, *http.Client, error) {
	return p, http.DefaultClient, nil
}

type albumCacheSettings struct {
	settings.Repository
	revision int64
}

func (r *albumCacheSettings) GetPluginSettings(context.Context, string, string) (settings.Record, error) {
	return settings.Record{Revision: r.revision}, nil
}

func TestAlbumCacheScopeRefreshAndExpiry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int32
		source := albumCachePlugin{"one", func(_ context.Context, _ string, _ scraper.Query) ([]scraper.Item, error) {
			return []scraper.Item{{ID: "release", Title: fmt.Sprint(calls.Add(1)), Tracks: []scraper.Track{{Title: "Song"}}}}, nil
		}}
		other := source
		other.id = "two"
		config := &albumCacheSettings{}
		service, err := onlinescrape.NewWithSettings([]scraper.Plugin{source, other}, config)
		if err != nil {
			t.Fatal(err)
		}
		query := scraper.Query{ExternalID: "release", SourceURL: "https://source.test/us/album/release"}
		read := func(owner, plugin, field string, query scraper.Query, refresh bool) onlinescrape.Candidate {
			t.Helper()
			items, err := service.SearchWithRefresh(t.Context(), owner, plugin, field, query, refresh)
			if err != nil || len(items) != 1 {
				t.Fatalf("unexpected search response: %v, %v", items, err)
			}
			return items[0]
		}
		first := read("alice", "one", "albumTracks", query, false)
		first.Tracks[0].Title = "Caller edit"
		time.Sleep(31 * time.Minute)
		cached := read("alice", "one", "albumTracks", query, false)
		if cached.Title != first.Title || cached.Token == first.Token || cached.Tracks[0].Title != "Song" || calls.Load() != 1 {
			t.Fatal("cache hit did not issue an independent preview")
		}
		if _, err := service.Candidate("alice", "one", "albumTracks", first.Token); err == nil {
			t.Fatal("expired preview token is still available")
		}
		if _, err := service.Candidate("alice", "one", "albumTracks", cached.Token); err != nil {
			t.Fatal("cache hit issued an unusable token", err)
		}
		if _, err := service.Candidate("bob", "one", "albumTracks", cached.Token); err == nil {
			t.Fatal("another owner can select the cached preview")
		}
		read("bob", "one", "albumTracks", query, false)
		read("alice", "two", "albumTracks", query, false)
		read("alice", "one", "albumDetails", query, false)
		region := query
		region.Country = "JP"
		read("alice", "one", "albumTracks", region, false)
		region = query
		region.SourceURL = "https://source.test/jp/album/release"
		read("alice", "one", "albumTracks", region, false)
		config.revision++
		read("alice", "one", "albumTracks", query, false)
		if calls.Load() != 7 {
			t.Fatal("different owners, settings, fields, stores or plugins shared a query", calls.Load())
		}
		fresh := read("alice", "one", "albumTracks", query, true)
		if got := read("alice", "one", "albumTracks", query, false); got.Title != fresh.Title || calls.Load() != 8 {
			t.Fatal("successful refresh did not replace the cached album")
		}
		time.Sleep(24 * time.Hour)
		if got := read("alice", "one", "albumTracks", query, false); got.Title == fresh.Title {
			t.Fatal("album cache failed to expire")
		}
		read("alice", "one", "trackDetails", query, false)
		read("alice", "one", "trackDetails", query, false)
		if calls.Load() != 11 {
			t.Fatal("track-level searches unexpectedly used the album cache")
		}
	})
}

func TestAlbumCacheFailureAndRemovedEdition(t *testing.T) {
	var mode string
	source := albumCachePlugin{"one", func(context.Context, string, scraper.Query) ([]scraper.Item, error) {
		switch mode {
		case "failed":
			return nil, errors.New("upstream failure")
		case "removed":
			return nil, nil
		default:
			return []scraper.Item{{ID: "id", Title: mode}}, nil
		}
	}}
	service, _ := onlinescrape.New([]scraper.Plugin{source})
	read := func(query scraper.Query, refresh bool) ([]onlinescrape.Candidate, error) {
		return service.SearchWithRefresh(t.Context(), "alice", "one", "albumDetails", query, refresh)
	}
	name, exact := scraper.Query{Title: "name"}, scraper.Query{ExternalID: "id"}
	mode = "summary"
	read(name, false)
	mode = "full"
	if got, err := read(exact, false); err != nil || len(got) != 1 || got[0].Title != "full" {
		t.Fatal("name search result shadowed the exact lookup", got, err)
	}
	mode = "failed"
	if _, err := read(exact, true); err == nil {
		t.Fatal("refresh hid a failure")
	}
	if got, err := read(exact, false); err != nil || len(got) != 1 || got[0].Title != "full" {
		t.Fatal("failed refresh replaced a good result", got, err)
	}
	mode = "removed"
	if got, err := read(exact, true); err != nil || len(got) != 0 {
		t.Fatal("removed release returned an old candidate", got, err)
	}
	if got, err := read(name, false); err != nil || len(got) != 0 {
		t.Fatal("keyword list retained removed release", got, err)
	}
	mode = "new"
	if got, err := read(name, false); err != nil || len(got) != 1 || got[0].Title != "new" {
		t.Fatal("empty result hid a later release", got, err)
	}
}

func TestAlbumCacheRefreshOnlyInvalidatesKnownEditions(t *testing.T) {
	var calls atomic.Int32
	source := albumCachePlugin{"one", func(_ context.Context, _ string, q scraper.Query) ([]scraper.Item, error) {
		calls.Add(1)
		if q.SourceURL != "" || q.Title == "removed" {
			return nil, nil
		}
		return []scraper.Item{{ID: q.Title, Title: q.Title, SourceURL: "https://source.test/" + q.Title}}, nil
	}}
	service, _ := onlinescrape.New([]scraper.Plugin{source})
	read := func(q scraper.Query, refresh bool) {
		t.Helper()
		if _, err := service.SearchWithRefresh(t.Context(), "alice", "one", "albumDetails", q, refresh); err != nil {
			t.Fatal(err)
		}
	}
	first, second := scraper.Query{Title: "first"}, scraper.Query{Title: "second"}
	read(first, false)
	read(second, false)
	read(scraper.Query{Title: "removed"}, true)
	read(first, false)
	read(second, false)
	if calls.Load() != 3 {
		t.Fatal("an unrelated empty search evicted cached editions")
	}
	read(scraper.Query{SourceURL: "https://source.test/first"}, true)
	read(second, false)
	read(first, false)
	if calls.Load() != 5 {
		t.Fatal("removed album page did not invalidate exactly its cached edition")
	}
}

func TestAlbumCacheEvictsOldestQueryByCountAndBytes(t *testing.T) {
	for _, tc := range []struct{ count, bytes int }{{513, 4}, {43, 800 << 10}} {
		title := strings.Repeat("x", tc.bytes)
		var calls atomic.Int32
		source := albumCachePlugin{"one", func(_ context.Context, _ string, q scraper.Query) ([]scraper.Item, error) {
			calls.Add(1)
			return []scraper.Item{{ID: q.ExternalID, Title: title}}, nil
		}}
		service, _ := onlinescrape.New([]scraper.Plugin{source})
		for i := range tc.count {
			_, err := service.Search(t.Context(), "alice", "one", "albumDetails", scraper.Query{ExternalID: strconv.Itoa(i)})
			if err != nil {
				t.Fatal(err)
			}
		}
		for _, id := range []int{tc.count - 1, 0} {
			if _, err := service.Search(t.Context(), "alice", "one", "albumDetails", scraper.Query{ExternalID: strconv.Itoa(id)}); err != nil {
				t.Fatal(err)
			}
		}
		if calls.Load() != int32(tc.count+1) {
			t.Fatal("cache did not keep the latest query and evict the oldest", tc, calls.Load())
		}
	}
}
