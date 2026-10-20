// Package rpcbackend adapts the complete persistence contract to a trusted
// executable plugin. Wire version 1 uses the Go SDK and portable SQLite backups.
package rpcbackend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"

	"github.com/fnMrRice/MusicFS-server/src/plugindb"
	"github.com/fnMrRice/MusicFS-server/src/pluginrpc"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
)

type Client struct{ *pluginrpc.Process }

var _ storage.Backend = (*Client)(nil)
var _ storage.Maintenance = (*Client)(nil)

func init() {
	if err := storage.Register(Driver()); err != nil {
		panic(err)
	}
}

func Driver() storage.Driver {
	portable := sqlite.Driver()
	return storage.Driver{
		Name: "plugin", Version: storage.ContractVersion,
		SnapshotName: portable.SnapshotName, RestoredName: portable.RestoredName,
		BackupSchemaVersion: portable.BackupSchemaVersion, InspectBackup: portable.InspectBackup,
		Open: func(ctx context.Context, options storage.Options) (storage.Backend, error) {
			return open(ctx, options, "backend", reflect.TypeFor[storage.Backend]())
		},
		OpenMaintenance: func(ctx context.Context, options storage.Options) (storage.Maintenance, error) {
			return open(ctx, options, "maintenance", reflect.TypeFor[storage.Maintenance]())
		},
	}
}

func open(ctx context.Context, options storage.Options, mode string, contract reflect.Type) (*Client, error) {
	cfg := pluginrpc.ProcessConfig{
		Executable: options.Executable, SHA256: options.SHA256,
		Args: options.Args, WorkDir: options.DataRoot,
	}
	// Do not pass the host's executable settings back into the backend factory.
	options.Executable, options.SHA256, options.Args = "", "", nil
	process, err := pluginrpc.Start(ctx, cfg, contract, mode, options)
	if err != nil {
		return nil, err
	}
	return &Client{Process: process}, nil
}

// Serve exposes the supplied implementation, preserving every repository
// operation as one call. Driver snapshots must satisfy the portable SQLite schema.
func Serve(ctx context.Context, input io.ReadCloser, output io.WriteCloser, driver storage.Driver) error {
	contracts := map[string]reflect.Type{
		"backend":           reflect.TypeFor[storage.Backend](),
		"maintenance":       reflect.TypeFor[storage.Maintenance](),
		"plugin-state":      reflect.TypeFor[plugindb.Backend](),
		"plugin-state-read": reflect.TypeFor[plugindb.Backend](),
	}
	return pluginrpc.Serve(ctx, input, output, contracts, func(ctx context.Context, mode string, raw json.RawMessage) (any, reflect.Type, io.Closer, error) {
		if mode == "plugin-state" || mode == "plugin-state-read" {
			return plugindb.WorkerFactory(ctx, mode, raw)
		}
		var options storage.Options
		if json.Unmarshal(raw, &options) != nil {
			return nil, nil, nil, fmt.Errorf("invalid persistence options")
		}
		switch mode {
		case "backend":
			store, err := driver.Open(ctx, options)
			return store, reflect.TypeFor[storage.Backend](), store, err
		case "maintenance":
			store, err := driver.OpenMaintenance(ctx, options)
			return store, reflect.TypeFor[storage.Maintenance](), store, err
		default:
			return nil, nil, nil, fmt.Errorf("unsupported persistence mode")
		}
	})
}
