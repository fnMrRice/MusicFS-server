package storage_test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/identification/hashworker"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

func TestHashSchedulerAutomaticOptInAndConcurrency(t *testing.T) {
	s := evidenceStore(t)
	one, two, three := evidenceObservation("one", "v1"), evidenceObservation("two", "v1"), evidenceObservation("three", "v1")
	one.SizeBytes, two.SizeBytes, three.SizeBytes = 3, 3, 3
	tracks := publishEvidenceTracks(t, s, one, two, three)
	source := &hashSource{store: s, data: []byte("abc"), opened: make(chan struct{}, 4)}
	manual := startHashes(t, s, source, hashworker.Config{MaxConcurrent: 1, BytesPerSecond: 1 << 30})
	time.Sleep(150 * time.Millisecond)
	manual.Close()
	source.mu.Lock()
	opens := source.opens
	source.mu.Unlock()
	if opens != 0 {
		t.Fatal("default configuration read files without an explicit task")
	}
	block := make(chan struct{})
	source.block = block
	automatic := startHashes(t, s, source, hashworker.Config{Auto: true, MaxConcurrent: 2, BytesPerSecond: 1 << 30})
	for i := 0; i < 2; i++ {
		select {
		case <-source.opened:
		case <-time.After(3 * time.Second):
			t.Fatal("automatic workers did not fill bounded lane")
		}
	}
	time.Sleep(50 * time.Millisecond)
	source.mu.Lock()
	active, max := source.active, source.maxActive
	source.mu.Unlock()
	if active != 2 || max != 2 {
		t.Fatalf("concurrency limit broken: active=%d maximum=%d", active, max)
	}
	close(block)
	deadline := time.Now().Add(5 * time.Second)
	for {
		all := true
		for _, track := range tracks {
			a, err := s.Asset(context.Background(), "admin", track.AssetID)
			if err != nil {
				t.Fatal(err)
			}
			all = all && a.HashStatus == "verified"
		}
		if all {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("automatic hashes did not finish")
		}
		time.Sleep(5 * time.Millisecond)
	}
	automatic.Close()
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.opens != 3 || source.closes != 3 || source.maxActive > 2 {
		t.Fatalf("unexpected reads or leaked guards: %+v", source)
	}
}

func TestHashWorkerShutdownRecoveryReplacesTicket(t *testing.T) {
	s := evidenceStore(t)
	o := evidenceObservation("restart", "v1")
	o.SizeBytes = 3
	track := publishEvidenceTracks(t, s, o)[0]
	blocked := make(chan struct{})
	source := &hashSource{store: s, data: []byte("abc"), block: blocked, opened: make(chan struct{}, 1)}
	worker := startHashes(t, s, source, hashworker.Config{MaxConcurrent: 1, BytesPerSecond: 1 << 30})
	j, _, err := worker.Submit(context.Background(), "admin", track.ID, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-source.opened:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	old, err := s.GetHashJob(context.Background(), "admin", j.ID)
	if err != nil {
		t.Fatal(err)
	}
	worker.Close()
	left, err := s.GetHashJob(context.Background(), "admin", j.ID)
	if err != nil || left.Status != "running" {
		t.Fatal("shutdown discarded durable work", err)
	}
	close(blocked)
	restarted := startHashes(t, s, source, hashworker.Config{MaxConcurrent: 1, BytesPerSecond: 1 << 30})
	finished := waitHash(t, restarted, j.ID)
	if finished.Status != "succeeded" || finished.Generation <= old.Generation || finished.Ticket.ID == old.Ticket.ID {
		t.Fatalf("recovery reused old execution: %+v", finished)
	}
	if err = s.FinishHash(context.Background(), old, hashResult(), old.FileSizeBytes); err == nil {
		t.Fatal("old execution published after restart")
	}
	restarted.Close()
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.opens != 2 || source.closes != 2 {
		t.Fatal("shutdown/recovery leaked read guards")
	}
}

type rejectFirstFinish struct {
	*sqlite.Store
	failed bool
}

func (r *rejectFirstFinish) FinishHash(ctx context.Context, j identification.HashJob, result identification.Result, n int64) error {
	if !r.failed {
		r.failed = true
		return errors.New("private storage diagnostic must not leak")
	}
	return r.Store.FinishHash(ctx, j, result, n)
}

func TestHashWorkerPublicationFailureSettlesWithoutFabricatedDigest(t *testing.T) {
	s := evidenceStore(t)
	o := evidenceObservation("failure", "v1")
	o.SizeBytes = 3
	track := publishEvidenceTracks(t, s, o)[0]
	source := &hashSource{store: s, data: []byte("abc")}
	repo := &rejectFirstFinish{Store: s}
	worker, err := hashworker.New(repo, source, hashworker.Config{MaxConcurrent: 1, BytesPerSecond: 1 << 30})
	if err != nil {
		t.Fatal(err)
	}
	if err = worker.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	j, _, err := worker.Submit(context.Background(), "admin", track.ID, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	finished := waitHash(t, worker, j.ID)
	if finished.Status != "failed" || finished.Error == nil || finished.Error.Message == "private storage diagnostic must not leak" {
		t.Fatalf("storage failure not settled safely: %+v", finished)
	}
	a, _ := s.Asset(context.Background(), "admin", track.AssetID)
	if a.HashStatus != "failed" || a.FileSHA512 != nil {
		t.Fatal("failed publication left verified evidence")
	}
}

func TestAutomaticHashCandidatesAdvancePastRejectedPage(t *testing.T) {
	s := evidenceStore(t)
	items := make([]library.Observation, 17)
	for i := range items {
		items[i] = evidenceObservation(fmt.Sprintf("track-%02d", i), "v1")
		items[i].SizeBytes = 3
	}
	tracks := publishEvidenceTracks(t, s, items...)
	sort.Slice(tracks, func(i, j int) bool { return tracks[i].ID < tracks[j].ID })
	source := &hashSource{store: s, data: []byte("abc"), rejected: map[string]bool{}}
	for _, track := range tracks[:16] {
		source.rejected[track.ID] = true
	}
	worker := startHashes(t, s, source, hashworker.Config{Auto: true, MaxConcurrent: 1, BytesPerSecond: 1 << 30})
	deadline := time.Now().Add(5 * time.Second)
	for {
		a, err := s.Asset(context.Background(), "admin", tracks[16].AssetID)
		if err != nil {
			t.Fatal(err)
		}
		if a.HashStatus == "verified" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("rejected first page starved a later eligible source")
		}
		time.Sleep(10 * time.Millisecond)
	}
	worker.Close()
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.opens != 1 || source.closes != 1 {
		t.Fatal("automatic hashing opened a rejected source")
	}
}
