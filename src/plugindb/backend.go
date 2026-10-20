package plugindb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/pluginrpc"
)

// Backend preserves the instance namespace and atomic compare-and-set contract.
type Backend interface {
	Get(context.Context, string) (pluginproto.StateValue, error)
	CompareAndSet(context.Context, pluginproto.StateRequest) (pluginproto.StateValue, error)
	Close() error
}

type BackendConfig struct {
	Name       string
	Executable string
	SHA256     string
	Args       []string
}

type stateOptions struct{ Path, Plugin, Instance string }
type remoteStore struct{ *pluginrpc.Process }

func (r *remoteStore) Get(ctx context.Context, key string) (result pluginproto.StateValue, err error) {
	err = r.Invoke(ctx, "Get", []any{key}, []any{&result})
	return
}
func (r *remoteStore) CompareAndSet(ctx context.Context, request pluginproto.StateRequest) (result pluginproto.StateValue, err error) {
	err = r.Invoke(ctx, "CompareAndSet", []any{request}, []any{&result})
	return
}

func OpenBackend(ctx context.Context, cfg BackendConfig, path, plugin, instance string) (Backend, error) {
	if cfg.Name != "plugin" {
		if cfg.Name != "" && cfg.Name != "sqlite" {
			return nil, fmt.Errorf("unsupported plugin state backend")
		}
		return Open(ctx, path, plugin, instance)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	return openRemote(ctx, cfg, path, plugin, instance, "plugin-state")
}

func ReadBackend(ctx context.Context, cfg BackendConfig, path, plugin, instance, key string) (pluginproto.StateValue, error) {
	if cfg.Name != "plugin" {
		if cfg.Name != "" && cfg.Name != "sqlite" {
			return pluginproto.StateValue{}, fmt.Errorf("unsupported plugin state backend")
		}
		return Read(ctx, path, plugin, instance, key)
	}
	remote, err := openRemote(ctx, cfg, path, plugin, instance, "plugin-state-read")
	if err != nil {
		return pluginproto.StateValue{}, err
	}
	defer remote.Close()
	return remote.Get(ctx, key)
}

func openRemote(ctx context.Context, cfg BackendConfig, path, plugin, instance, mode string) (*remoteStore, error) {
	process, err := pluginrpc.Start(ctx, pluginrpc.ProcessConfig{
		Executable: cfg.Executable, SHA256: cfg.SHA256, Args: cfg.Args, WorkDir: filepath.Dir(path),
	}, reflect.TypeFor[Backend](), mode, stateOptions{path, plugin, instance})
	if err != nil {
		return nil, err
	}
	return &remoteStore{process}, nil
}

type readStore struct{ options stateOptions }

func (r *readStore) Get(ctx context.Context, key string) (pluginproto.StateValue, error) {
	return Read(ctx, r.options.Path, r.options.Plugin, r.options.Instance, key)
}
func (r *readStore) CompareAndSet(context.Context, pluginproto.StateRequest) (pluginproto.StateValue, error) {
	return pluginproto.StateValue{}, fmt.Errorf("plugin state is read-only before startup")
}
func (r *readStore) Close() error { return nil }

// WorkerFactory is the private-state half of the persistence SDK. Read mode
// performs no migration or creation before the host acquires its data lock.
func WorkerFactory(ctx context.Context, mode string, raw json.RawMessage) (any, reflect.Type, io.Closer, error) {
	var options stateOptions
	if json.Unmarshal(raw, &options) != nil || !filepath.IsAbs(options.Path) || options.Plugin == "" || options.Instance == "" {
		return nil, nil, nil, fmt.Errorf("invalid plugin state options")
	}
	var store Backend
	var err error
	switch mode {
	case "plugin-state":
		store, err = Open(ctx, options.Path, options.Plugin, options.Instance)
	case "plugin-state-read":
		store = &readStore{options}
	default:
		err = fmt.Errorf("unsupported plugin state mode")
	}
	return store, reflect.TypeFor[Backend](), store, err
}
