package pluginhost

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func TestDatabaseRPCScopeAndProtectedConfiguration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "plugins.sqlite")
	a := &Worker{config: Config{InstanceID: "one", DatabasePath: path}}
	b := &Worker{config: Config{InstanceID: "two", DatabasePath: path}}
	r := pluginproto.StateRequest{Key: "preferences", Value: []byte(`{"recursive":true}`)}
	raw, _ := json.Marshal(r)
	if _, err := a.handle(ctx, "host.database.compareAndSet", raw); err != nil {
		t.Fatal(err)
	}
	if _, err := a.handle(ctx, "host.database.compareAndSet", raw); pluginproto.ErrorCode(err) != "RESOURCE_CHANGED" {
		t.Fatalf("RPC conflict was not preserved: %v", err)
	}
	v, err := b.handle(ctx, "host.database.get", raw)
	if err != nil || v.(pluginproto.StateValue).Revision != 0 {
		t.Fatal("RPC crossed instance boundary", err)
	}
	r.Key = "host-configuration"
	raw, _ = json.Marshal(r)
	if _, err := a.handle(ctx, "host.database.compareAndSet", raw); err == nil {
		t.Fatal("RPC bypassed directory permission and lifecycle validation")
	}
}
