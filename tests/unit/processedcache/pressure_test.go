package processedcache_test

import (
	"context"
	"errors"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/processedcache"
)

type failedDeleteRepository struct{ memory }

func (*failedDeleteRepository) ForgetProcessedCache(context.Context, string) error {
	return errors.New("synthetic database unavailable")
}

func TestZeroByteAbortsRemainBoundedWhenIndexDeleteFails(t *testing.T) {
	r := &failedDeleteRepository{memory{records: map[string]processedcache.Record{}}}
	b, _ := processedcache.NewBudget(1024)
	m, err := processedcache.New(context.Background(), r, b, processedcache.Config{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	for range processedcache.MaxEntries {
		w := m.Begin(identity())
		if w == nil {
			t.Fatal("premature cache admission failure")
		}
		w.Abort()
		m.Collect(context.Background())
	}
	if m.Begin(identity()) != nil || b.Used() != 0 {
		t.Fatal("unbounded cleanup metadata or zero-byte accounting")
	}
}

func TestPressureEvictsOnlyUnreferencedOutput(t *testing.T) {
	m, b, r, _, _ := setup(t, 16)
	p := complete(t, m, r, identity(), "0123456789")
	m.MakeRoom(context.Background(), 8)
	if b.TryAcquire(8) {
		t.Fatal("active attempt was evicted")
	}
	p.Close()
	m.MakeRoom(context.Background(), 8)
	if !b.TryAcquire(8) {
		t.Fatal("unused output blocked a new input")
	}
	b.Release(8)
}
