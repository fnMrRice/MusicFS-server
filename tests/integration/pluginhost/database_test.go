package pluginhost_test

import (
	"context"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestDatabaseRPCScopeAndProtectedConfiguration(t *testing.T) {
	cfg := localConfig(t, "database")
	a := startLocal(t, cfg)
	cfg.InstanceID = "16b003e5-1bab-52bb-be10-0e83ec1c40ea"
	cfg.StateDir = t.TempDir()
	b := startLocal(t, cfg)
	call := func(w interface {
		Resolve(context.Context, pluginproto.ResolveRequest) (pluginproto.Resource, error)
	}, operation string) (pluginproto.Resource, error) {
		return w.Resolve(t.Context(), pluginproto.ResolveRequest{ExternalID: operation})
	}
	if _, err := call(a, "compareAndSet:preferences"); err != nil {
		t.Fatal(err)
	}
	if _, err := call(a, "compareAndSet:preferences"); pluginproto.ErrorCode(err) != "RESOURCE_CHANGED" {
		t.Fatalf("RPC conflict was not preserved: %v", err)
	}
	if v, err := call(b, "get:preferences"); err != nil || v.SizeBytes != 0 {
		t.Fatal("RPC crossed instance boundary", err)
	}
	if _, err := call(a, "compareAndSet:host-configuration"); err == nil {
		t.Fatal("RPC bypassed directory permission and lifecycle validation")
	}
}
