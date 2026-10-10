package plugincontrol_test

import (
	"context"
	"testing"
	"time"
)

func TestForceThenEnableCannotReuseCanceledGeneration(t *testing.T) {
	h := setup(t)
	gate := h.manager.Gate("ncm")
	epoch := gate.Epoch()
	lease, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	disable := change(t, h, "force-first", false, "force")
	enable := change(t, h, "enable-next", true, "drain")
	// Keep the canceled resource alive: enabling cannot reopen its generation.
	time.Sleep(100 * time.Millisecond)
	if gate.Accepting() || h.starts.Load() != 0 {
		t.Fatal("canceled generation reopened before resource cleanup")
	}
	lease.Close()
	waitStatus(t, h, disable, "superseded")
	waitStatus(t, h, enable, "succeeded")
	if h.starts.Load() != 1 || h.stops.Load() != 1 || gate.Epoch() == epoch {
		t.Fatal("force did not rebuild the worker")
	}
}
