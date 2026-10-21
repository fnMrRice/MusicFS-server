package processedcache_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/playback/processedcache"
)

func TestRestartRestoresVerifiedCompleteOutputAndDeletesOrphans(t *testing.T) {
	m, _, r, root, _ := setup(t, 1024)
	p := complete(t, m, r, identity(), "complete-output")
	p.Close()
	m.Close()
	orphan := filepath.Join(root, strings.Repeat("a", 48)+".part")
	os.WriteFile(orphan, []byte("partial"), 0600)
	b, _ := processedcache.NewBudget(1024)
	restarted, err := processedcache.New(context.Background(), r, b, processedcache.Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	l := restarted.Acquire(identity())
	if l == nil {
		t.Fatal("complete cache lost on restart")
	}
	id := l.Record.ID
	l.Close()
	if _, err = os.Stat(orphan); !os.IsNotExist(err) || b.Used() != 15 {
		t.Fatal("orphan restored or remained accounted")
	}
	restarted.Close()
	if err = os.WriteFile(filepath.Join(root, id+".mp3"), []byte("changed-output!"), 0600); err != nil {
		t.Fatal(err)
	}
	b, _ = processedcache.NewBudget(1024)
	corrupt, err := processedcache.New(context.Background(), r, b, processedcache.Config{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	defer corrupt.Close()
	if l = corrupt.Acquire(identity()); l != nil {
		l.Close()
		t.Fatal("corrupted bytes restored")
	}
	if b.Used() != 0 || len(r.records) != 0 {
		t.Fatal("corrupt cache not removed")
	}
}

func TestInputReservationsAndOutputShareOneBudget(t *testing.T) {
	m, b, r, _, _ := setup(t, 16)
	if !b.TryAcquire(12) {
		t.Fatal("input reservation rejected")
	}
	w := m.Begin(identity())
	w.Write([]byte("output-data"))
	if _, err := w.Complete(context.Background(), func(context.Context, processedcache.Record) error {
		t.Fatal("output bypassed input reservation")
		return nil
	}); err == nil {
		t.Fatal("cache unexpectedly complete")
	}
	if b.Used() != 12 {
		t.Fatal("failed output changed input accounting")
	}
	b.Release(12)
	p := complete(t, m, r, identity(), "output-data")
	p.Close()
	if b.TryAcquire(6) {
		t.Fatal("new input bypassed completed output accounting")
	}
	if b.Release(100) || b.Used() != 11 {
		t.Fatal("budget underflow")
	}
}

func TestSurroundCacheRestoresItsWebMFileAndProfile(t *testing.T) {
	for _, profile := range []string{"opus-5.1", "opus-7.1"} {
		t.Run(profile, func(t *testing.T) {
			manager, _, repo, root, _ := setup(t, 1024)
			current := identity()
			current.Profile = profile
			pin := complete(t, manager, repo, current, "surround-output")
			pin.Close()
			manager.Close()
			budget, _ := processedcache.NewBudget(1024)
			restored, err := processedcache.New(t.Context(), repo, budget, processedcache.Config{Root: root})
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Close()
			lease := restored.Acquire(current)
			if lease == nil {
				t.Fatal("surround output did not survive cache restart")
			}
			defer lease.Close()
			data, err := io.ReadAll(lease.File)
			if err != nil || string(data) != "surround-output" || filepath.Ext(lease.File.Name()) != ".webm" {
				t.Fatal("surround cache restored the wrong representation", lease.File.Name(), err)
			}
			other := current
			other.Profile = "mp3-128"
			if stale := restored.Acquire(other); stale != nil {
				stale.Close()
				t.Fatal("stereo session reused multichannel WebM cache")
			}
		})
	}
}
