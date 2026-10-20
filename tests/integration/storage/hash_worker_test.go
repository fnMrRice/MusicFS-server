package storage_test

import (
	"bytes"
	"context"
	"crypto/sha512"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/identification/hashworker"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type hashSource struct {
	store                            *sqlite.Store
	data                             []byte
	mode                             string
	mu                               sync.Mutex
	opens, closes, active, maxActive int
	opened                           chan struct{}
	block                            <-chan struct{}
	rejected                         map[string]bool
}

func (s *hashSource) ResolveHashTarget(ctx context.Context, owner, id string) (identification.HashTarget, error) {
	if s.rejected[id] {
		return identification.HashTarget{}, domain.Fail("UNSUPPORTED", "Source cannot be read completely")
	}
	t, err := s.store.FindTrack(ctx, owner, id)
	if err != nil {
		return identification.HashTarget{}, err
	}
	return identification.HashTarget{SourceTrackID: t.ID, AssetID: t.AssetID, AssetVersion: t.Version, FileSizeBytes: t.Size}, nil
}
func (s *hashSource) AutoHashScopes() []identification.HashScope {
	return []identification.HashScope{{Owner: "admin", ProviderInstanceID: "local"}}
}
func (s *hashSource) OpenStableHash(ctx context.Context, owner, task string, target identification.HashTarget) (identification.StableInput, error) {
	if s.mode == "unsupported" {
		return nil, domain.Fail("UNSUPPORTED", "Stable reads are unavailable")
	}
	s.mu.Lock()
	s.opens++
	s.active++
	if s.active > s.maxActive {
		s.maxActive = s.active
	}
	s.mu.Unlock()
	if s.opened != nil {
		select {
		case s.opened <- struct{}{}:
		default:
		}
	}
	return &hashInput{source: s, ctx: ctx, reader: bytes.NewReader(s.data)}, nil
}

type hashInput struct {
	source      *hashSource
	ctx         context.Context
	reader      *bytes.Reader
	offset      int64
	validations int
	closed      bool
}

func (h *hashInput) ReadAt(p []byte, off int64) (int, error) {
	if off != h.offset || len(p) > 64<<10 {
		return 0, fmt.Errorf("invalid sequential coverage")
	}
	if h.source.block != nil {
		select {
		case <-h.ctx.Done():
			return 0, h.ctx.Err()
		case <-h.source.block:
		}
	}
	if h.source.mode == "short" {
		return 0, io.ErrUnexpectedEOF
	}
	n, err := h.reader.ReadAt(p, off)
	h.offset += int64(n)
	return n, err
}
func (h *hashInput) Validate() error {
	h.validations++
	if h.closed {
		return fmt.Errorf("closed before validation")
	}
	if h.source.mode == "changed" && h.validations > 1 {
		return domain.Fail("RESOURCE_CHANGED", "Source changed")
	}
	return h.ctx.Err()
}
func (h *hashInput) Publish(fn func() error) error {
	if err := h.Validate(); err != nil {
		return err
	}
	if h.source.mode == "publish-change" {
		return domain.Fail("RESOURCE_CHANGED", "Source changed before publication")
	}
	return fn()
}
func (h *hashInput) Close() error {
	if h.closed {
		return fmt.Errorf("input closed twice")
	}
	h.closed = true
	h.source.mu.Lock()
	h.source.closes++
	h.source.active--
	h.source.mu.Unlock()
	return nil
}

func startHashes(t *testing.T, s *sqlite.Store, source *hashSource, c hashworker.Config) *hashworker.Service {
	t.Helper()
	worker, err := hashworker.New(s, source, c)
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(worker.Close)
	return worker
}
func waitHash(t *testing.T, worker *hashworker.Service, id string) identification.HashJob {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		j, err := worker.Get(context.Background(), "admin", id)
		if err != nil {
			t.Fatal(err)
		}
		if j.Status != "queued" && j.Status != "running" {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("hash task did not finish")
	return identification.HashJob{}
}

func TestHashWorkerKnownVectorsAndCompleteContainer(t *testing.T) {
	ncm, err := testkit.BuildNCMWithOptions([]byte("payload"), nil, testkit.NCMOptions{Cover: []byte("cover"), CoverPadding: 1024})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		data  []byte
		known string
	}{
		{"empty", nil, "cf83e1357eefb8bdf1542850d66d8007d620e4050b5715dc83f4a921d36ce9ce47d0d13c5d85f2b0ff8318d2877eec2f63b931bd47417a81a538327af927da3e"},
		{"abc", []byte("abc"), "ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f"},
		{"chunk-boundary", bytes.Repeat([]byte("unique-block-0123456789"), 7000), ""},
		{"original-ncm-cover", ncm, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := evidenceStore(t)
			o := evidenceObservation(tc.name, "v1")
			o.SizeBytes = int64(len(tc.data))
			track := publishEvidenceTracks(t, s, o)[0]
			source := &hashSource{store: s, data: tc.data}
			worker := startHashes(t, s, source, hashworker.Config{MaxConcurrent: 1, BytesPerSecond: 1 << 30})
			job, _, err := worker.Submit(context.Background(), "admin", track.ID, domain.NewID())
			if err != nil {
				t.Fatal(err)
			}
			finished := waitHash(t, worker, job.ID)
			if finished.Status != "succeeded" || finished.Progress.CompletedBytes != o.SizeBytes {
				t.Fatalf("hash failed: %+v", finished)
			}
			a, err := s.Asset(context.Background(), "admin", track.AssetID)
			if err != nil {
				t.Fatal(err)
			}
			expected := tc.known
			if expected == "" {
				expected = fmt.Sprintf("%x", sha512.Sum512(tc.data))
			}
			if a.FileSHA512 == nil || *a.FileSHA512 != expected || a.HashStatus != "verified" {
				t.Fatalf("wrong complete-file hash: %+v", a)
			}
			worker.Close()
			source.mu.Lock()
			defer source.mu.Unlock()
			if source.opens != 1 || source.closes != 1 {
				t.Fatalf("read guard leaked: %d/%d", source.opens, source.closes)
			}
		})
	}
}

func TestHashWorkerRejectsIncompleteOrUnstableReads(t *testing.T) {
	for _, mode := range []string{"short", "changed", "publish-change", "unsupported", "extra"} {
		t.Run(mode, func(t *testing.T) {
			s := evidenceStore(t)
			o := evidenceObservation("invalid", "v1")
			o.SizeBytes = 3
			track := publishEvidenceTracks(t, s, o)[0]
			data := []byte("abc")
			if mode == "extra" {
				data = []byte("abcd")
			}
			source := &hashSource{store: s, data: data, mode: mode}
			worker := startHashes(t, s, source, hashworker.Config{MaxConcurrent: 1, BytesPerSecond: 1 << 30})
			job, _, err := worker.Submit(context.Background(), "admin", track.ID, domain.NewID())
			if err != nil {
				t.Fatal(err)
			}
			finished := waitHash(t, worker, job.ID)
			want := "failed"
			if mode == "unsupported" {
				want = "unavailable"
			}
			if finished.Status != want || finished.Error == nil {
				t.Fatalf("invalid read accepted: %+v", finished)
			}
			a, _ := s.Asset(context.Background(), "admin", track.AssetID)
			if a.FileSHA512 != nil || a.HashComputedAt != nil || a.HashStatus != want {
				t.Fatal("failed read left a verified digest")
			}
		})
	}
}

func TestHashWorkerCancellationAndIndependentScan(t *testing.T) {
	s := evidenceStore(t)
	o := evidenceObservation("slow", "v1")
	o.SizeBytes = 128 << 10
	track := publishEvidenceTracks(t, s, o)[0]
	source := &hashSource{store: s, data: make([]byte, o.SizeBytes), opened: make(chan struct{}, 1)}
	worker := startHashes(t, s, source, hashworker.Config{MaxConcurrent: 1, BytesPerSecond: 65536})
	job, _, err := worker.Submit(context.Background(), "admin", track.ID, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-source.opened:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not open source")
	}
	// A foreground scan can publish while a long hash is waiting for its budget.
	start := time.Now()
	publishEvidenceTracks(t, s, o, evidenceObservation("new-track", "v1"))
	if time.Since(start) > time.Second {
		t.Fatal("hash blocked scan publication")
	}
	start = time.Now()
	cancelled, err := worker.Cancel(context.Background(), "admin", job.ID)
	if err != nil || cancelled.Status != "cancelled" {
		t.Fatal("cancel failed", err)
	}
	worker.Close()
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("rate-limited cancellation was not prompt")
	}
	again, err := worker.Cancel(context.Background(), "admin", job.ID)
	if err != nil || again.Status != "cancelled" || !again.UpdatedAt.Equal(cancelled.UpdatedAt) {
		t.Fatal("cancel was not naturally idempotent", err)
	}
	a, _ := s.Asset(context.Background(), "admin", track.AssetID)
	if a.FileSHA512 != nil || a.HashStatus != "failed" {
		t.Fatal("cancelled task published a digest")
	}
}
