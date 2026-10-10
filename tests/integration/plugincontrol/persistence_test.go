package plugincontrol_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
)

type countedOperations struct {
	plugincontrol.Repository
	attempts atomic.Int32
	failures atomic.Int32
}

func (r *countedOperations) SavePluginOperation(ctx context.Context, op plugincontrol.Operation) error {
	r.attempts.Add(1)
	if r.failures.Add(-1) >= 0 {
		return errors.New("synthetic operation persistence failure")
	}
	return r.Repository.SavePluginOperation(ctx, op)
}

func TestPluginOperationPersistsChangesAndRetriesFailedWritesOnly(t *testing.T) {
	for _, failFirstWrites := range []int32{0, 3} {
		t.Run(map[bool]string{true: "retry-initial-and-unchanged-failure", false: "unchanged-drain-is-not-written"}[failFirstWrites > 0], func(t *testing.T) {
			var repo *countedOperations
			h := setup(t, func(base plugincontrol.Repository) plugincontrol.Repository {
				repo = &countedOperations{Repository: base}
				repo.failures.Store(failFirstWrites)
				return repo
			})
			lease, err := h.manager.Gate("ncm").Acquire(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close()
			op, err := h.manager.Change(context.Background(), "admin", config.Change{ExpectedRevision: h.manager.Snapshot().Revision, IdempotencyKey: "persist-operation", PluginID: "musicfs.ncm", Enabled: false})
			if failFirstWrites == 0 && err != nil {
				t.Fatal(err)
			}
			if failFirstWrites > 0 {
				code(t, err, "TEMPORARY_FAILURE")
			}
			waitStatus(t, h, op, "draining")
			want := int32(2)
			if failFirstWrites > 0 {
				want = failFirstWrites + 1
			}
			if got := repo.attempts.Load(); got != want {
				t.Fatalf("operation save attempts = %d, wanted %d", got, want)
			}
			// Hold admitted work across several reconciliation ticks.
			time.Sleep(220 * time.Millisecond)
			if got := repo.attempts.Load(); got != want {
				t.Fatalf("unchanged draining operation was written again: %d attempts", got)
			}
			lease.Close()
			waitStatus(t, h, op, "succeeded")
			if got := repo.attempts.Load(); got != want+1 {
				t.Fatalf("terminal transition was not persisted exactly once: %d attempts", got)
			}
		})
	}
}
