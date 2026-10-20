package plugincontrol_test

import (
	"context"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
)

func stateChange(t *testing.T, h *harness, instance, key string, enabled bool, mode string) plugincontrol.Operation {
	t.Helper()
	op, err := h.manager.Change(context.Background(), "admin", config.Change{
		ExpectedRevision: h.manager.Snapshot().Revision, IdempotencyKey: key,
		PluginID: "musicfs.plugin.decoder.ncm", InstanceID: instance, Enabled: enabled, Mode: mode,
	})
	if err != nil {
		t.Fatal(err)
	}
	return op
}

func waitApplied(t *testing.T, manager *plugincontrol.Manager) plugincontrol.Snapshot {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); ; {
		snapshot := manager.Snapshot()
		if snapshot.AppliedRevision == snapshot.Revision {
			return snapshot
		}
		if time.Now().After(deadline) {
			t.Fatal("configuration revision was not applied", snapshot)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestDisableSavesIntentWithoutChangingRuntimeUntilServerRestart(t *testing.T) {
	for _, target := range []string{"plugin", "instance"} {
		for _, mode := range []string{"drain", "force"} {
			t.Run(target+"-"+mode, func(t *testing.T) {
				h := setup(t)
				initial := waitApplied(t, h.manager)
				gate := h.manager.Gate(ncmInstanceID)
				epoch := gate.Epoch()
				lease, err := gate.Acquire(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				defer lease.Close()
				instance := ""
				if target == "instance" {
					instance = ncmInstanceID
				}
				op := stateChange(t, h, instance, "save-disable", false, mode)
				waitStatus(t, h, op, "succeeded")
				if !op.RestartRequired {
					t.Fatal("saved disable omitted restart requirement")
				}
				persisted, err := h.manager.Operation(context.Background(), "admin", op.ID)
				if err != nil || !persisted.RestartRequired {
					t.Fatal("operation did not persist restart requirement", persisted, err)
				}
				for _, ctx := range []context.Context{context.Background(), lease.Context()} {
					additional, err := gate.AcquireEpoch(ctx, epoch)
					if err != nil {
						t.Fatal("saved disable blocked current-generation work", err)
					}
					additional.Close()
				}
				// Cross reconciliation ticks so a delayed stop cannot pass unnoticed.
				time.Sleep(120 * time.Millisecond)
				snapshot := h.manager.Snapshot()
				state := snapshot.Items[0].Instances[0]
				if !snapshot.RestartRequired || snapshot.AppliedRevision != initial.AppliedRevision || snapshot.Revision == initial.Revision {
					t.Fatal("saved disable was reported as applied", snapshot)
				}
				if state.RuntimeState != "ready" || !state.EffectiveEnabled || !state.AcceptingNewWork || state.Generation != initial.Items[0].Instances[0].Generation || state.Reason == nil || *state.Reason != "RESTART_REQUIRED" {
					t.Fatal("saved disable changed runtime state", state)
				}
				if lease.Context().Err() != nil || h.starts.Load() != 0 || h.stops.Load() != 0 || gate.Epoch() != epoch {
					t.Fatal("saved disable canceled work or replaced the worker")
				}
				if target == "plugin" && snapshot.Items[0].ConfiguredEnabled || target == "instance" && state.ConfiguredEnabled {
					t.Fatal("disable intent was not reflected in configured state")
				}
				if _, err = h.manager.Operation(context.Background(), "other", op.ID); err == nil {
					t.Fatal("operation owner leaked")
				}
				lease.Close()
				h.manager.Close()
				store, err := config.OpenStore(context.Background(), h.path, nil, h.repo.(config.Journal))
				if err != nil {
					t.Fatal(err)
				}
				restarted, err := plugincontrol.New(context.Background(), store.Current(), store, h.repo)
				if err != nil {
					t.Fatal(err)
				}
				defer restarted.Close()
				if err = restarted.Register(ncmInstanceID, plugincontrol.Hooks{}, false); err != nil {
					t.Fatal(err)
				}
				restarted.Start()
				after := waitApplied(t, restarted)
				state = after.Items[0].Instances[0]
				if after.RestartRequired || state.RuntimeState != "disabled" || state.EffectiveEnabled || state.AcceptingNewWork {
					t.Fatal("server restart did not apply saved disable", after)
				}
				code(t, restarted.Gate(ncmInstanceID).Check(context.Background()), "PLUGIN_DISABLED")
				persisted, err = restarted.Operation(context.Background(), "admin", op.ID)
				if err != nil || persisted.Status != "succeeded" || !persisted.RestartRequired {
					t.Fatal("restart lost the saved operation outcome", persisted, err)
				}
			})
		}
	}
}

func TestUndoDisableKeepsTheCurrentWorkerAndGeneration(t *testing.T) {
	for _, instance := range []string{"", ncmInstanceID} {
		t.Run("instance="+instance, func(t *testing.T) {
			h := setup(t)
			initial := waitApplied(t, h.manager)
			gate := h.manager.Gate(ncmInstanceID)
			epoch := gate.Epoch()
			lease, err := gate.Acquire(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer lease.Close()
			disable := stateChange(t, h, instance, "force-first", false, "force")
			enable := stateChange(t, h, instance, "enable-next", true, "drain")
			waitStatus(t, h, disable, "succeeded")
			waitStatus(t, h, enable, "succeeded")
			after := waitApplied(t, h.manager)
			if after.RestartRequired || enable.RestartRequired || !gate.Accepting() || lease.Context().Err() != nil {
				t.Fatal("undoing saved disable disrupted active work", after)
			}
			if h.starts.Load() != 0 || h.stops.Load() != 0 || gate.Epoch() != epoch || after.Items[0].Instances[0].Generation != initial.Items[0].Instances[0].Generation {
				t.Fatal("undoing saved disable replaced the worker")
			}
		})
	}
}
