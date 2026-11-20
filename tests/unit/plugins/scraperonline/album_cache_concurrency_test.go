package scraperonline_test

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
)

func TestCachedAlbumAvailableWhenSearchCapacityIsFull(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var calls atomic.Int32
		source := albumCachePlugin{"one", func(_ context.Context, _ string, q scraper.Query) ([]scraper.Item, error) {
			calls.Add(1)
			if q.ExternalID != "cached" {
				<-release
			}
			return []scraper.Item{{ID: q.ExternalID, Title: "Album"}}, nil
		}}
		service, _ := onlinescrape.New([]scraper.Plugin{source})
		read := func(id string) error {
			_, err := service.Search(t.Context(), "alice", "one", "albumDetails", scraper.Query{ExternalID: id})
			return err
		}
		if err := read("cached"); err != nil {
			t.Fatal(err)
		}
		for i := range 8 {
			go func() { _ = read(strconv.Itoa(i)) }()
		}
		synctest.Wait()
		if err := read("cached"); err != nil || calls.Load() != 9 {
			t.Error("a cache hit consumed upstream search capacity", err)
		}
		close(release)
	})
}

func TestConcurrentAlbumSearchSharesUpstreamUntilLastReaderLeaves(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls, cancelled atomic.Int32
		release := make(chan struct{})
		source := albumCachePlugin{"one", func(ctx context.Context, _ string, q scraper.Query) ([]scraper.Item, error) {
			calls.Add(1)
			select {
			case <-ctx.Done():
				cancelled.Add(1)
				return nil, ctx.Err()
			case <-release:
				return []scraper.Item{{ID: q.ExternalID, Title: "Album"}}, nil
			}
		}}
		service, _ := onlinescrape.New([]scraper.Plugin{source})
		query := scraper.Query{ExternalID: "release"}
		read := func(ctx context.Context) <-chan error {
			done := make(chan error, 1)
			go func() {
				_, err := service.Search(ctx, "alice", "one", "albumDetails", query)
				done <- err
			}()
			return done
		}
		first, cancelFirst := context.WithCancel(t.Context())
		second, cancelSecond := context.WithCancel(t.Context())
		a, b := read(first), read(second)
		synctest.Wait()
		cancelFirst()
		synctest.Wait()
		if !errors.Is(<-a, context.Canceled) || calls.Load() != 1 || cancelled.Load() != 0 {
			t.Fatal("one cancelled reader affected another reader or duplicated the lookup")
		}
		cancelSecond()
		synctest.Wait()
		if !errors.Is(<-b, context.Canceled) || cancelled.Load() != 1 {
			t.Fatal("upstream lookup outlived its final reader")
		}
		close(release)
		if err := <-read(t.Context()); err != nil || calls.Load() != 2 {
			t.Fatal("new reader reused the cancelled lookup", err)
		}
	})
}

func TestLateKeywordSearchCannotOverwriteManualAlbumRefresh(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		blocked, release := make(chan struct{}), make(chan struct{})
		var keywordCalls atomic.Int32
		source := albumCachePlugin{"one", func(_ context.Context, _ string, q scraper.Query) ([]scraper.Item, error) {
			title := "New"
			if q.Title != "" && keywordCalls.Add(1) == 1 {
				close(blocked)
				<-release
				title = "Old"
			}
			return []scraper.Item{{ID: "release", Title: title}}, nil
		}}
		service, _ := onlinescrape.New([]scraper.Plugin{source})
		keyword := scraper.Query{Title: "find"}
		done := make(chan error, 1)
		go func() {
			_, err := service.Search(t.Context(), "alice", "one", "albumDetails", keyword)
			done <- err
		}()
		<-blocked
		_, err := service.SearchWithRefresh(t.Context(), "alice", "one", "albumDetails", scraper.Query{ExternalID: "release"}, true)
		if err != nil {
			t.Fatal(err)
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		items, err := service.Search(t.Context(), "alice", "one", "albumDetails", keyword)
		if err != nil || len(items) != 1 || items[0].Title != "New" || keywordCalls.Load() != 2 {
			t.Fatal("late search repopulated the pre-refresh edition", items, err)
		}
	})
}

func TestAlbumReadDuringRefreshUsesTheExistingSnapshot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		blocked, release := make(chan struct{}), make(chan struct{})
		var calls atomic.Int32
		source := albumCachePlugin{"one", func(context.Context, string, scraper.Query) ([]scraper.Item, error) {
			if calls.Add(1) == 2 {
				close(blocked)
				<-release
				return nil, errors.New("unavailable")
			}
			return []scraper.Item{{ID: "release", Title: "Album"}}, nil
		}}
		service, _ := onlinescrape.New([]scraper.Plugin{source})
		query := scraper.Query{ExternalID: "release"}
		read := func(refresh bool) error {
			_, err := service.SearchWithRefresh(t.Context(), "alice", "one", "albumDetails", query, refresh)
			return err
		}
		if err := read(false); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- read(true) }()
		<-blocked
		if err := read(false); err != nil || calls.Load() != 2 {
			t.Fatal("cached album unavailable while refresh was in progress", err)
		}
		close(release)
		if err := <-done; err == nil {
			t.Fatal("refresh hid the upstream error")
		}
	})
}
