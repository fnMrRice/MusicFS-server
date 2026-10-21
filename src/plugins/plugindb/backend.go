package plugindb

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/state"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginrpc"
)

const ConfigurationKey = "host-configuration"

// Backend 保留实例命名空间与原子比较并写入语义。
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

// OpenFactory 由数据库驱动提供，在宿主取得数据锁后完成初始化和迁移。
type OpenFactory func(context.Context, BackendConfig, string, string, string) (Backend, error)

// ReadFactory 由数据库驱动提供，在取得数据锁之前只读访问已有状态。
type ReadFactory func(context.Context, BackendConfig, string, string, string, string) (pluginproto.StateValue, error)

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
		return state.Open(ctx, path, plugin, instance)
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
		return state.Read(ctx, path, plugin, instance, key)
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

type readStore struct {
	options stateOptions
	read    ReadFactory
}

func (r *readStore) Get(ctx context.Context, key string) (pluginproto.StateValue, error) {
	return r.read(ctx, BackendConfig{}, r.options.Path, r.options.Plugin, r.options.Instance, key)
}
func (r *readStore) CompareAndSet(context.Context, pluginproto.StateRequest) (pluginproto.StateValue, error) {
	return pluginproto.StateValue{}, fmt.Errorf("plugin state is read-only before startup")
}
func (r *readStore) Close() error { return nil }

// WorkerFactory 将私有状态请求交给所选数据库驱动，读取模式不会调用初始化工厂。
func WorkerFactory(ctx context.Context, mode string, raw json.RawMessage, open OpenFactory, read ReadFactory) (any, reflect.Type, io.Closer, error) {
	var options stateOptions
	if json.Unmarshal(raw, &options) != nil || !filepath.IsAbs(options.Path) || options.Plugin == "" || options.Instance == "" {
		return nil, nil, nil, fmt.Errorf("invalid plugin state options")
	}
	var store Backend
	var err error
	switch mode {
	case "plugin-state":
		if open == nil {
			return nil, nil, nil, fmt.Errorf("plugin state initialization is not supported")
		}
		store, err = open(ctx, BackendConfig{}, options.Path, options.Plugin, options.Instance)
	case "plugin-state-read":
		if read == nil {
			return nil, nil, nil, fmt.Errorf("plugin state reads are not supported")
		}
		store = &readStore{options: options, read: read}
	default:
		err = fmt.Errorf("unsupported plugin state mode")
	}
	missing := store == nil || reflect.ValueOf(store).Kind() == reflect.Pointer && reflect.ValueOf(store).IsNil()
	if err != nil {
		if !missing {
			_ = store.Close()
		}
		return nil, nil, nil, err
	}
	if missing {
		return nil, nil, nil, fmt.Errorf("plugin state factory returned no store")
	}
	return store, reflect.TypeFor[Backend](), store, nil
}
