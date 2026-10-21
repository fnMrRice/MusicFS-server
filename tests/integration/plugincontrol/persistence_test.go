package plugincontrol_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
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
			lease, err := h.manager.Gate(ncmInstanceID).Acquire(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close()
			name := "Persisted decoder name"
			op, err := h.manager.Change(context.Background(), "admin", config.Change{ExpectedRevision: h.manager.Snapshot().Revision, IdempotencyKey: "persist-operation", PluginID: "musicfs.plugin.decoder.ncm", InstanceID: ncmInstanceID, Settings: &config.InstanceSettings{Name: &name}})
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

func TestSavedDisableRetriesItsOperationRecordWithoutRuntimeChanges(t *testing.T) {
	var repo *countedOperations
	h := setup(t, func(base plugincontrol.Repository) plugincontrol.Repository {
		repo = &countedOperations{Repository: base}
		repo.failures.Store(1)
		return repo
	})
	op, err := h.manager.Change(context.Background(), "admin", config.Change{
		ExpectedRevision: h.manager.Snapshot().Revision, IdempotencyKey: "persist-saved-disable",
		PluginID: "musicfs.plugin.decoder.ncm", Enabled: false, Mode: "force",
	})
	code(t, err, "TEMPORARY_FAILURE")
	if op.Status != "succeeded" || !op.RestartRequired || h.store.Current().Plugins[0].Enabled {
		t.Fatal("failed operation confirmation lost the saved disable", op)
	}
	waitStatus(t, h, op, "succeeded")
	persisted, err := h.manager.Operation(context.Background(), "admin", op.ID)
	if err != nil || !persisted.RestartRequired || repo.attempts.Load() != 2 {
		t.Fatal("terminal operation was not retried with its restart requirement", persisted, err)
	}
	if !h.manager.Gate(ncmInstanceID).Accepting() || h.starts.Load() != 0 || h.stops.Load() != 0 {
		t.Fatal("retrying a saved disable changed the current runtime")
	}
}
