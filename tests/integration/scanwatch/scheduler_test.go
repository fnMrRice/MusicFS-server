package scanwatch_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library/scanwatch"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type submission struct {
	input library.ScanInput
	id    string
}
type scanner struct {
	mu         sync.Mutex
	status     string
	calls      int
	lastStatus string
	states     map[string]string
	submitted  chan submission
}

func (s *scanner) Scan(_ context.Context, owner, instance string, input library.ScanInput, key string) (jobs.Job, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if owner != "admin" || instance != "local" || len(key) < 8 {
		panic("watch scope lost")
	}
	s.calls++
	j := jobs.Job{ID: fmt.Sprint(s.calls), Status: s.status}
	if s.states == nil {
		s.states = map[string]string{}
	}
	s.states[j.ID] = j.Status
	s.submitted <- submission{input: input, id: j.ID}
	return j, false, nil
}
func (s *scanner) Job(_ context.Context, _, id string) (jobs.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastStatus = s.states[id]
	return jobs.Job{ID: id, Status: s.states[id]}, nil
}
func (s *scanner) set(status string) {
	s.mu.Lock()
	for id := range s.states {
		s.states[id] = status
	}
	s.mu.Unlock()
}
func (s *scanner) count() int { s.mu.Lock(); defer s.mu.Unlock(); return s.calls }
func nextSubmission(t *testing.T, s *scanner) submission {
	t.Helper()
	select {
	case got := <-s.submitted:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not submit scan")
		return submission{}
	}
}
func watchOptions(t *testing.T, root string) scanwatch.Options {
	t.Helper()
	a, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	plan := testkit.ScanPlan("root")
	plan.Roots[0].RootIdentity = a.Roots()[0].ExpectedIdentity
	return scanwatch.Options{Owner: "admin", InstanceID: "local", Plan: plan, Access: a, Debounce: 50 * time.Millisecond, MaxDelay: 200 * time.Millisecond, FullInterval: time.Hour, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestWatchCoalescesWhileRunningAndStopsAfterPaused(t *testing.T) {
	root := t.TempDir()
	o := watchOptions(t, root)
	s := &scanner{status: "running", submitted: make(chan submission, 16)}
	w, err := scanwatch.Start(context.Background(), s, o)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if first := nextSubmission(t, s); first.input.Mode != "full" {
		t.Fatal("startup did not reconcile")
	}
	for i := 0; i < 100; i++ {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("file-%d.wav", i)), []byte("synthetic"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// Event processing is concurrent; holding the active job proves no extra jobs are submitted.
	time.Sleep(300 * time.Millisecond)
	if s.count() != 1 {
		t.Fatal("events created jobs while another scan was active")
	}
	s.set("succeeded")
	second := nextSubmission(t, s)
	if second.input.Mode != "incremental" || len(second.input.RootIDs) != 1 || second.input.RootIDs[0] != "root" {
		t.Fatal("coalesced hint lost scope")
	}
	s.set("paused")
	select {
	case <-w.Done():
	case <-time.After(3 * time.Second):
		s.mu.Lock()
		status := s.lastStatus
		calls := s.calls
		s.mu.Unlock()
		t.Fatalf("paused scan kept scheduling; last observed=%s submissions=%d", status, calls)
	}
	if s.count() != 2 {
		t.Fatal("paused scan was automatically replaced")
	}
}

func TestWatchDirectoryLimitFallsBackToPeriodicFull(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < scanwatch.MaxDirectories; i++ {
		if err := os.Mkdir(filepath.Join(root, fmt.Sprintf("dir-%03d", i)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	o := watchOptions(t, root)
	o.FullInterval = 100 * time.Millisecond
	s := &scanner{status: "succeeded", submitted: make(chan submission, 16)}
	w, err := scanwatch.Start(context.Background(), s, o)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if nextSubmission(t, s).input.Mode != "full" || nextSubmission(t, s).input.Mode != "full" {
		t.Fatal("unavailable native notifications lost periodic reconciliation")
	}
	start := time.Now()
	w.Close()
	if time.Since(start) > time.Second {
		t.Fatal("watch shutdown did not cancel promptly")
	}
}

func TestWatchBurstHasBoundedMaximumDelay(t *testing.T) {
	root := t.TempDir()
	o := watchOptions(t, root)
	o.Debounce = 200 * time.Millisecond
	o.MaxDelay = 400 * time.Millisecond
	s := &scanner{status: "succeeded", submitted: make(chan submission, 16)}
	w, err := scanwatch.Start(context.Background(), s, o)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	nextSubmission(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(15 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				_ = os.WriteFile(filepath.Join(root, "changing.wav"), []byte(time.Now().String()), 0600)
			}
		}
	}()
	defer func() { cancel(); <-done }()
	if nextSubmission(t, s).input.Mode != "incremental" {
		t.Fatal("continuous changes were not coalesced into a scan")
	}
}
