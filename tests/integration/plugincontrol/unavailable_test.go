package plugincontrol_test

import (
	"context"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
)

func TestPluginGateOfflineCausePreservesAdmissionAndForceCleanup(t *testing.T) {
	gate := plugincontrol.NewGate(true)
	lease, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gate.SetUnavailable("SOURCE_OFFLINE", true)
	oldEpoch := gate.Epoch()
	gate.Rotate()
	code(t, gate.Check(context.Background()), "SOURCE_OFFLINE")
	_, err = gate.Acquire(context.Background())
	code(t, err, "SOURCE_OFFLINE")
	_, err = gate.AcquireEpoch(context.Background(), oldEpoch)
	code(t, err, "SOURCE_OFFLINE")
	if gate.Active() != 1 || lease.Context().Err() == nil {
		t.Fatal("offline force removed lease before actual cleanup")
	}
	lease.Close()
	gate.SetAccepting(false, false)
	code(t, gate.Check(context.Background()), "PLUGIN_DISABLED")
	_, err = gate.AcquireEpoch(context.Background(), oldEpoch)
	code(t, err, "PLUGIN_DISABLED")
	gate.SetAccepting(true, false)
	_, err = gate.AcquireEpoch(context.Background(), oldEpoch)
	code(t, err, "RESOURCE_CHANGED")
}

func TestPluginManagerWorkerExitRemainsOfflineUntilExplicitStateChange(t *testing.T) {
	h := setup(t)
	gate := h.manager.Gate("ncm")
	h.ready.Store(false)
	for deadline := time.Now().Add(2 * time.Second); ; {
		state := h.manager.Snapshot().Items[0].Instances[0]
		if state.RuntimeState == "failed" && h.stops.Load() == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("worker crash was not reclaimed", state)
		}
		time.Sleep(5 * time.Millisecond)
	}
	code(t, gate.Check(context.Background()), "SOURCE_OFFLINE")
	disabled := change(t, h, "disable-failed-worker", false, "drain")
	waitStatus(t, h, disabled, "succeeded")
	code(t, gate.Check(context.Background()), "PLUGIN_DISABLED")
	enabled := change(t, h, "restart-failed-worker", true, "drain")
	waitStatus(t, h, enabled, "succeeded")
	if err := gate.Check(context.Background()); err != nil {
		t.Fatal("recovered worker retained offline cause", err)
	}
}
