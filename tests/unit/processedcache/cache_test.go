package processedcache_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/playback/processedcache"
)

type memory struct {
	mu      sync.Mutex
	records map[string]processedcache.Record
}

func (r *memory) ListProcessedCache(context.Context) ([]processedcache.Record, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := []processedcache.Record{}
	for _, item := range r.records {
		result = append(result, item)
	}
	return result, nil
}
func (r *memory) PublishProcessedCache(_ context.Context, item processedcache.Record, _ processedcache.Authorization) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records[item.ID] = item
	return nil
}
func (r *memory) CheckProcessedCache(context.Context, processedcache.Record, processedcache.Authorization) error {
	return nil
}
func (r *memory) ForgetProcessedCache(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.records, id)
	return nil
}
func identity() processedcache.Identity {
	return processedcache.Identity{Owner: "owner", ProviderID: "local", SourceTrackID: "track", AssetID: "asset", Version: "version", PluginID: "musicfs.plugin.decoder.ffmpeg", InputSHA256: strings.Repeat("1", 64), WorkerSHA256: strings.Repeat("2", 64), DecoderSHA256: strings.Repeat("3", 64), PluginVersion: "0.1.0", PipelineRevision: "gain-v1", Profile: "mp3-128", PositionMs: 0}
}
func setup(t *testing.T, limit int64) (*processedcache.Manager, *processedcache.Budget, *memory, string, *atomic.Int64) {
	t.Helper()
	root := t.TempDir()
	r := &memory{records: map[string]processedcache.Record{}}
	b, _ := processedcache.NewBudget(limit)
	clock := &atomic.Int64{}
	clock.Store(time.Now().UTC().UnixNano())
	m, err := processedcache.New(context.Background(), r, b, processedcache.Config{Root: root, IdleTTL: time.Minute, Now: func() time.Time { return time.Unix(0, clock.Load()) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m, b, r, root, clock
}
func complete(t *testing.T, m *processedcache.Manager, r *memory, i processedcache.Identity, data string) *processedcache.Pin {
	t.Helper()
	w := m.Begin(i)
	if w == nil {
		t.Fatal("writer rejected")
	}
	w.Write([]byte(data))
	p, err := w.Complete(context.Background(), func(ctx context.Context, item processedcache.Record) error {
		return r.PublishProcessedCache(ctx, item, processedcache.Authorization{})
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCompletedCacheIsolationAndReferenceGC(t *testing.T) {
	m, b, r, _, clock := setup(t, 1024)
	i := identity()
	p := complete(t, m, r, i, "complete-output")
	if b.Used() != 15 {
		t.Fatal("output not accounted")
	}
	lease := m.Acquire(i)
	if lease == nil {
		t.Fatal("missing complete output")
	}
	data, _ := io.ReadAll(lease.File)
	if string(data) != "complete-output" {
		t.Fatal("cache bytes changed")
	}
	for _, change := range []func(*processedcache.Identity){
		func(i *processedcache.Identity) { i.Owner = "another" }, func(i *processedcache.Identity) { i.ProviderID = "another" },
		func(i *processedcache.Identity) { i.AssetID = "other" }, func(i *processedcache.Identity) { i.Version = "other" },
		func(i *processedcache.Identity) { i.InputSHA256 = strings.Repeat("4", 64) }, func(i *processedcache.Identity) { i.WorkerSHA256 = strings.Repeat("4", 64) },
		func(i *processedcache.Identity) { i.DecoderSHA256 = strings.Repeat("4", 64) }, func(i *processedcache.Identity) { i.PositionMs = 1000 },
		func(i *processedcache.Identity) { i.PluginID = "independent.decoder" },
		func(i *processedcache.Identity) { i.GainDB = -6 }, func(i *processedcache.Identity) { i.PluginVersion = "0.2.0" },
	} {
		candidate := i
		change(&candidate)
		if got := m.Acquire(candidate); got != nil {
			got.Close()
			t.Fatal("private processing identity was shared")
		}
	}
	clock.Add(int64(2 * time.Minute))
	m.Collect(context.Background())
	if b.Used() != 15 {
		t.Fatal("active output was collected")
	}
	p.Close()
	m.Collect(context.Background())
	if b.Used() != 15 {
		t.Fatal("HTTP reference lost")
	}
	lease.Close()
	if lease.Pin() != nil {
		t.Fatal("closed lease revived")
	}
	clock.Add(int64(2 * time.Minute))
	m.Collect(context.Background())
	if b.Used() != 0 {
		t.Fatal("idle output retained")
	}
}

func TestPartialAndOverBudgetOutputNeverPublishOrBreakWriter(t *testing.T) {
	m, b, r, root, _ := setup(t, 8)
	w := m.Begin(identity())
	if w == nil {
		t.Fatal("writer missing")
	}
	if duplicate := m.Begin(identity()); duplicate != nil {
		t.Fatal("duplicate cache generation admitted")
	}
	if n, err := w.Write([]byte("123456789")); n != 9 || err != nil {
		t.Fatal("cache budget broke delivery")
	}
	called := false
	if _, err := w.Complete(context.Background(), func(context.Context, processedcache.Record) error { called = true; return nil }); err == nil || called {
		t.Fatal("over-budget output published")
	}
	w.Abort()
	m.Collect(context.Background())
	files, _ := os.ReadDir(root)
	if b.Used() != 0 || len(files) != 0 || len(r.records) != 0 {
		t.Fatal("partial cache leaked")
	}
	w = m.Begin(identity())
	w.Write([]byte("1234"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := w.Complete(ctx, func(context.Context, processedcache.Record) error { t.Fatal("cancelled output published"); return nil }); err == nil {
		t.Fatal("cancel ignored")
	}
	if b.Used() != 0 {
		t.Fatal("cancelled cache remained accounted")
	}
}

func TestManagerCloseCancelsPendingPublication(t *testing.T) {
	m, b, _, _, _ := setup(t, 1024)
	w := m.Begin(identity())
	w.Write([]byte("complete"))
	entered := make(chan struct{})
	finished := make(chan error, 1)
	go func() {
		_, err := w.Complete(context.Background(), func(ctx context.Context, _ processedcache.Record) error {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		})
		finished <- err
	}()
	select {
	case <-entered:
	case err := <-finished:
		t.Fatalf("publication did not start: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("publication did not start")
	}
	closed := make(chan struct{})
	go func() { m.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("cache close blocked on publication")
	}
	if err := <-finished; err == nil || b.Used() != 0 {
		t.Fatal("drained output remained publishable")
	}
}

func TestFailedUnlinkRetainsQuotaUntilActualRemoval(t *testing.T) {
	m, b, _, root, _ := setup(t, 16)
	w := m.Begin(identity())
	w.Write([]byte("12345678"))
	var path string
	_, err := w.Complete(context.Background(), func(_ context.Context, r processedcache.Record) error {
		path = filepath.Join(root, r.ID+".audio")
		if err := os.Remove(path); err != nil {
			return err
		}
		if err := os.Mkdir(path, 0700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(path, "blocker"), []byte("test"), 0600); err != nil {
			return err
		}
		return errors.New("publication refused")
	})
	if err == nil || path == "" {
		t.Fatal("unexpected publication")
	}
	m.Collect(context.Background())
	if b.Used() != 8 {
		t.Fatal("failed unlink released quota")
	}
	if err = os.Remove(filepath.Join(path, "blocker")); err != nil {
		t.Fatal(err)
	}
	m.Collect(context.Background())
	if b.Used() != 0 {
		t.Fatal("removed cache still counted")
	}
}
