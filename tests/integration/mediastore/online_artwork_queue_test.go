package mediastore_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlineartwork"
)

type queueTracks struct{ entered chan struct{} }

func (t queueTracks) Track(ctx context.Context, owner, id string) (library.Track, error) {
	select {
	case t.entered <- struct{}{}:
	case <-ctx.Done():
		return library.Track{}, ctx.Err()
	}
	return library.Track{ID: id, ExternalID: id, Owner: owner, ProviderID: "net", RootID: "online-audio", Availability: "available"}, nil
}

type queueMetadata struct {
	artProvider
	started chan struct{}
	release chan struct{}
	stopped chan struct{}
	active  atomic.Int32
	peak    atomic.Int32
	first   atomic.Bool
	retry   bool
}

func (m *queueMetadata) MetadataBatch(ctx context.Context, _, _ string, ids []string) (online.TrackBatch, error) {
	n := m.active.Add(1)
	defer m.active.Add(-1)
	for old := m.peak.Load(); old < n; old = m.peak.Load() {
		if m.peak.CompareAndSwap(old, n) {
			break
		}
	}
	if m.first.CompareAndSwap(false, true) {
		close(m.started)
		select {
		case <-m.release:
		case <-ctx.Done():
			close(m.stopped)
			return online.TrackBatch{}, ctx.Err()
		}
		if m.retry {
			sec := 1
			return online.TrackBatch{}, &domain.Error{Code: "RATE_LIMITED", Message: "Test cooldown", RetryAfter: &sec}
		}
	}
	m.mu.Lock()
	m.batches = append(m.batches, append([]string(nil), ids...))
	m.mu.Unlock()
	b := online.TrackBatch{}
	for _, id := range ids {
		b.Items = append(b.Items, online.TrackLookup{ExternalID: id})
	}
	return b, nil
}
func pendingMetadata() *queueMetadata {
	return &queueMetadata{started: make(chan struct{}), release: make(chan struct{}), stopped: make(chan struct{})}
}
func TestOnlineArtworkQueuesLegacyCatalogWithoutStarvingTheWorker(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	const count = 113
	tracks := queueTracks{entered: make(chan struct{}, count)}
	p := pendingMetadata()
	p.retry = true
	s := onlineartwork.New(tracks, p, nil, nil)
	done := make(chan error, count)
	request := func(i int) { _, _, err := s.Open(ctx, "owner", fmt.Sprint(i)); done <- err }
	go request(1)
	select {
	case <-p.started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for i := 2; i <= count; i++ {
		go request(i)
	}
	for range count {
		select {
		case <-tracks.entered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	close(p.release)
	for range count {
		select {
		case err := <-done:
			var reason *domain.Error
			if !errors.As(err, &reason) || reason.Code != "NOT_FOUND" {
				t.Fatalf("queued request failed before metadata completed: %v", err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	seen := map[string]bool{}
	for _, ids := range p.batches {
		if len(ids) > online.MaxPageItems {
			t.Fatal("oversized cover batch")
		}
		for _, id := range ids {
			if seen[id] {
				t.Fatal("duplicate metadata read")
			}
			seen[id] = true
		}
	}
	if len(seen) != count || p.peak.Load() != 1 || len(p.batches) >= count {
		t.Fatalf("metadata reads=%d batches=%d peak concurrency=%d", len(seen), len(p.batches), p.peak.Load())
	}
}
func TestCanceledArtworkBrowserStopsMetadata(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	p := pendingMetadata()
	s := onlineartwork.New(queueTracks{entered: make(chan struct{}, 1)}, p, nil, nil)
	done := make(chan error, 1)
	go func() { _, _, err := s.Open(ctx, "owner", "1"); done <- err }()
	select {
	case <-p.started:
	case <-time.After(2 * time.Second):
		t.Fatal("metadata not reached")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-p.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("abandoned metadata continued using worker capacity")
	}
}

type waitingContent struct {
	started chan struct{}
	release chan struct{}
	sent    bool
}

func (r *waitingContent) Read(p []byte) (int, error) {
	if r.sent {
		return 0, io.EOF
	}
	r.sent = true
	close(r.started)
	<-r.release
	return copy(p, "content"), nil
}

func TestBackgroundArtworkUploadsWaitAndLeaveCapacityForInteractiveUploads(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	start := func(background bool) (*waitingContent, chan error) {
		r := &waitingContent{started: make(chan struct{}), release: make(chan struct{})}
		done := make(chan error, 1)
		upload := f.service.Upload
		if background {
			upload = f.service.UploadBackground
		}
		go func() { _, err := upload(ctx, "admin", f.metadata, "text/plain", domain.NewID(), r); done <- err }()
		return r, done
	}
	wait := func(r *waitingContent) {
		select {
		case <-r.started:
		case <-time.After(2 * time.Second):
			t.Fatal("upload not admitted")
		}
	}
	a, first := start(true)
	wait(a)
	b, second := start(true)
	c, manual := start(false)
	wait(c)
	_, err := f.service.Upload(ctx, "admin", f.metadata, "text/plain", domain.NewID(), strings.NewReader("busy"))
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != "RATE_LIMITED" {
		t.Fatal("interactive upload capacity changed", err)
	}
	close(a.release)
	wait(b)
	close(b.release)
	close(c.release)
	for _, ch := range []chan error{first, second, manual} {
		if err := <-ch; err != nil {
			t.Fatal(err)
		}
	}
}
