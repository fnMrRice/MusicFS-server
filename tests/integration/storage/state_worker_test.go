package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugindb"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginrpc"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/src/storage/rpcbackend"
)

type driverState struct{ closed int }

func (*driverState) Get(context.Context, string) (pluginproto.StateValue, error) {
	return pluginproto.StateValue{Revision: 17, Value: json.RawMessage(`"driver-state"`)}, nil
}
func (*driverState) CompareAndSet(context.Context, pluginproto.StateRequest) (pluginproto.StateValue, error) {
	return pluginproto.StateValue{Revision: 18, Value: json.RawMessage(`"driver-write"`)}, nil
}
func (s *driverState) Close() error { s.closed++; return nil }

func stateWorkerEndpoint(t *testing.T, driver storage.Driver, mode, path string) *pluginrpc.Endpoint {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	left, right := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- rpcbackend.Serve(ctx, right, right, driver) }()
	endpoint := pluginrpc.NewEndpoint()
	contract := reflect.TypeFor[plugindb.Backend]()
	if err := endpoint.SetContract(contract); err != nil {
		t.Fatal(err)
	}
	endpoint.Peer = pluginproto.NewPeerWithLimit(ctx, left, left, "h-", endpoint.Handle, pluginrpc.MaxMessageBytes)
	t.Cleanup(func() {
		endpoint.Peer.Close()
		cancel()
		if err := <-done; err != nil {
			t.Errorf("state worker shutdown: %v", err)
		}
	})
	hello := map[string]any{
		"Protocol": pluginrpc.Protocol, "Contract": pluginrpc.Fingerprint(contract),
		"Nonce": strings.Repeat("0", 48), "Mode": mode,
		"Options": map[string]any{"Path": path, "Plugin": "custom-plugin", "Instance": "custom-instance", "DataRoot": filepath.Dir(path), "Options": map[string]string{"tenant": "custom"}},
	}
	var ready map[string]any
	if err := endpoint.Peer.Call(ctx, "persistence.hello", hello, &ready); err != nil {
		t.Fatal(err)
	}
	return endpoint
}

func TestPersistenceWorkerUsesDriverStateFactories(t *testing.T) {
	for _, mode := range []string{"plugin-state", "plugin-state-read"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "uncreated", "state.db")
			driver := sqlite.Driver()
			openCalls, readCalls := 0, 0
			store := &driverState{}
			verify := func(cfg plugindb.BackendConfig, gotPath, plugin, instance string) {
				want := plugindb.BackendConfig{DataRoot: filepath.Dir(path), Options: map[string]string{"tenant": "custom"}}
				if !reflect.DeepEqual(cfg, want) || gotPath != path || plugin != "custom-plugin" || instance != "custom-instance" {
					t.Errorf("state factory arguments changed: %+v %q %q %q", cfg, gotPath, plugin, instance)
				}
			}
			driver.OpenPluginState = func(_ context.Context, cfg plugindb.BackendConfig, path, plugin, instance string) (plugindb.Backend, error) {
				verify(cfg, path, plugin, instance)
				openCalls++
				return store, nil
			}
			driver.ReadPluginState = func(_ context.Context, cfg plugindb.BackendConfig, path, plugin, instance, key string) (pluginproto.StateValue, error) {
				verify(cfg, path, plugin, instance)
				if key != "settings" {
					t.Errorf("state key changed: %q", key)
				}
				readCalls++
				return pluginproto.StateValue{Revision: 29, Value: json.RawMessage(`"driver-read"`)}, nil
			}
			endpoint := stateWorkerEndpoint(t, driver, mode, path)
			ctx := context.Background()
			var value pluginproto.StateValue
			if err := endpoint.Invoke(ctx, "Get", []any{"settings"}, []any{&value}); err != nil {
				t.Fatal(err)
			}
			if mode == "plugin-state" {
				if openCalls != 1 || readCalls != 0 || value.Revision != 17 {
					t.Fatalf("initialization factory was bypassed: open=%d read=%d value=%+v", openCalls, readCalls, value)
				}
			} else {
				if openCalls != 0 || readCalls != 1 || value.Revision != 29 {
					t.Fatalf("read factory was bypassed: open=%d read=%d value=%+v", openCalls, readCalls, value)
				}
				if err := endpoint.Invoke(ctx, "CompareAndSet", []any{pluginproto.StateRequest{Key: "settings", Value: json.RawMessage(`null`)}}, []any{&value}); err == nil {
					t.Fatal("read-only worker accepted a write")
				}
			}
			if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("state worker unexpectedly initialized SQLite: %v", err)
			}
		})
	}
}

func TestStateWorkerRejectsMissingStoresAndClosesFailedInitialization(t *testing.T) {
	raw, err := json.Marshal(map[string]string{"Path": filepath.Join(t.TempDir(), "state.db"), "Plugin": "custom", "Instance": "instance"})
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range []plugindb.Backend{nil, (*driverState)(nil)} {
		open := func(context.Context, plugindb.BackendConfig, string, string, string) (plugindb.Backend, error) {
			return result, nil
		}
		service, _, closer, err := plugindb.WorkerFactory(context.Background(), "plugin-state", raw, open, nil)
		if err == nil || service != nil || closer != nil {
			t.Fatalf("missing store accepted: %T %T %v", service, closer, err)
		}
	}
	partial := &driverState{}
	want := errors.New("state initialization failed")
	open := func(context.Context, plugindb.BackendConfig, string, string, string) (plugindb.Backend, error) {
		return partial, want
	}
	service, _, closer, err := plugindb.WorkerFactory(context.Background(), "plugin-state", raw, open, nil)
	if !errors.Is(err, want) || service != nil || closer != nil || partial.closed != 1 {
		t.Fatalf("failed initialization retained a resource: %T %T %v closed=%d", service, closer, err, partial.closed)
	}
}
