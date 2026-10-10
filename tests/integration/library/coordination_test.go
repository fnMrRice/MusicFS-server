package library_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestProviderCoordinationReadCancellationAndBarriers(t *testing.T) {
	lib := library.New(nil, jobs.New(nil), []library.Runtime{{ID: "one", Owner: "owner"}, {ID: "two", Owner: "owner"}}, nil)
	ctx := context.Background()
	job := domain.NewID()
	read, err := lib.HoldRead(ctx, "owner", "one")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lib.TryExclusive(ctx, "owner", "one", job); err == nil {
		t.Fatal("writer entered active read")
	}
	other, err := lib.TryExclusive(ctx, "owner", "two", job)
	if err != nil {
		t.Fatal("independent provider blocked", err)
	}
	other.Close()
	read.Close()
	read.Close()
	exclusive, err := lib.TryExclusive(ctx, "owner", "one", job)
	if err != nil {
		t.Fatal(err)
	}
	if !lib.WritebackPending("owner", "one", job) || lib.WritebackPending("owner", "one", "different") || lib.WritebackPending("another", "one", job) {
		t.Fatal("writeback completion scope is incorrect")
	}
	canceled, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	if _, err = lib.HoldRead(canceled, "owner", "one"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting read was not canceled: %v", err)
	}
	if err = exclusive.Block(); err != nil {
		t.Fatal(err)
	}
	exclusive.Close()
	if _, err = lib.HoldRead(ctx, "owner", "one"); err == nil {
		t.Fatal("recovery barrier bypassed")
	}
	if _, err = lib.TryExclusive(ctx, "owner", "one", domain.NewID()); err == nil {
		t.Fatal("another writer bypassed recovery")
	}
	exclusive, err = lib.TryExclusive(ctx, "owner", "one", job)
	if err != nil {
		t.Fatal(err)
	}
	if err = exclusive.ClearBarrier(); err != nil {
		t.Fatal(err)
	}
	exclusive.Close()
	if err = exclusive.ClearBarrier(); err == nil {
		t.Fatal("closed lease changed barrier")
	}
	if lib.WritebackPending("owner", "one", job) {
		t.Fatal("released writeback still reports pending")
	}
	read, err = lib.HoldRead(ctx, "owner", "one")
	if err != nil {
		t.Fatal(err)
	}
	read.Close()
	if _, err = lib.HoldRead(ctx, "another", "one"); err == nil {
		t.Fatal("foreign owner acquired read")
	}
	if err = lib.BlockProvider("owner", "one", job); err != nil {
		t.Fatal(err)
	}
	if _, _, err = lib.Scan(ctx, "owner", "one", library.ScanInput{}, "blocked-scan"); err == nil {
		t.Fatal("scan queued behind unresolved recovery")
	}
}

func TestOpenAndScrapeHoldUntilRevoked(t *testing.T) {
	lib, repo, _ := scrapeFixture(t, "track.mp3", testkit.ScraperID3(4, testkit.ScraperFrame{ID: "USLT", Body: []byte("\x03eng\x00Lyrics")}))
	ctx, cancel := context.WithCancel(context.Background())
	h, err := lib.Open(ctx, repo.track, "ordinary-read")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lib.TryExclusive(context.Background(), "owner", "provider", domain.NewID()); err == nil {
		t.Fatal("Open did not retain task read")
	}
	cancel()
	h.Close()
	if _, err = lib.TryExclusive(context.Background(), "owner", "provider", domain.NewID()); err == nil {
		t.Fatal("cancel released read before cleanup/revoke")
	}
	lib.Revoke("provider", "ordinary-read")
	input, err := lib.OpenScrape(context.Background(), "owner", "scrape", "track", []string{"lyrics"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lib.TryExclusive(context.Background(), "owner", "provider", domain.NewID()); err == nil {
		t.Fatal("scrape did not retain read")
	}
	input.Close()
	exclusive, err := lib.TryExclusive(context.Background(), "owner", "provider", domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	exclusive.Close()
}

func TestProviderCoordinationConcurrentReaders(t *testing.T) {
	lib := library.New(nil, jobs.New(nil), []library.Runtime{{ID: "one", Owner: "owner"}}, nil)
	var wg sync.WaitGroup
	entered := make(chan struct{}, 32)
	release := make(chan struct{})
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := lib.HoldRead(context.Background(), "owner", "one")
			if err != nil {
				t.Error(err)
				return
			}
			entered <- struct{}{}
			<-release
			r.Close()
		}()
	}
	for range 32 {
		<-entered
	}
	if _, err := lib.TryExclusive(context.Background(), "owner", "one", domain.NewID()); err == nil {
		t.Fatal("writer entered concurrent reads")
	}
	close(release)
	wg.Wait()
	w, err := lib.TryExclusive(context.Background(), "owner", "one", domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
}
