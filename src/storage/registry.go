package storage

import (
	"context"
	"maps"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sync"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

var backendName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)

type Registry struct {
	mu      sync.RWMutex
	drivers map[string]Driver
}

func NewRegistry() *Registry { return &Registry{drivers: map[string]Driver{}} }

var Default = NewRegistry()

func Register(driver Driver) error       { return Default.Register(driver) }
func Lookup(name string) (Driver, error) { return Default.Lookup(name) }
func Open(ctx context.Context, name string, options Options) (Backend, error) {
	return Default.Open(ctx, name, options)
}
func OpenMaintenance(ctx context.Context, name string, options Options) (Maintenance, error) {
	return Default.OpenMaintenance(ctx, name, options)
}

func snapshotName(name string) bool {
	return name != "" && name != "." && filepath.IsLocal(name) && filepath.Base(name) == name
}

// Register only accepts a complete matching contract and never replaces an
// existing implementation. Registration is intended for trusted server modules.
func (r *Registry) Register(driver Driver) error {
	if !backendName.MatchString(driver.Name) || driver.Version != ContractVersion ||
		!snapshotName(driver.SnapshotName) || !snapshotName(driver.RestoredName) ||
		driver.Open == nil || driver.OpenMaintenance == nil ||
		driver.BackupSchemaVersion == nil || driver.InspectBackup == nil {
		return domain.Fail("INVALID_ARGUMENT", "Invalid storage backend registration")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.drivers == nil {
		r.drivers = map[string]Driver{}
	}
	if _, exists := r.drivers[driver.Name]; exists {
		return domain.Fail("CONFLICT", "Storage backend is already registered")
	}
	r.drivers[driver.Name] = driver
	return nil
}

func (r *Registry) Lookup(name string) (Driver, error) {
	if name == "" {
		name = "sqlite"
	}
	r.mu.RLock()
	driver, exists := r.drivers[name]
	r.mu.RUnlock()
	if !exists {
		return Driver{}, domain.Fail("UNSUPPORTED", "Storage backend is not registered")
	}
	return driver, nil
}

func copyOptions(options Options) (Options, error) {
	if !filepath.IsAbs(options.DataRoot) {
		return Options{}, domain.Fail("INVALID_ARGUMENT", "Storage data root must be absolute")
	}
	options.Values = maps.Clone(options.Values)
	options.Args = slices.Clone(options.Args)
	return options, nil
}

func missing(value any) bool {
	return value == nil || reflect.ValueOf(value).Kind() == reflect.Pointer && reflect.ValueOf(value).IsNil()
}

func (r *Registry) Open(ctx context.Context, name string, options Options) (Backend, error) {
	driver, err := r.Lookup(name)
	if err != nil {
		return nil, err
	}
	options, err = copyOptions(options)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	store, err := driver.Open(ctx, options)
	if err != nil {
		if !missing(store) {
			_ = store.Close()
		}
		return nil, err
	}
	if missing(store) {
		return nil, domain.Fail("TEMPORARY_FAILURE", "Storage backend returned no store")
	}
	return store, nil
}

func (r *Registry) OpenMaintenance(ctx context.Context, name string, options Options) (Maintenance, error) {
	driver, err := r.Lookup(name)
	if err != nil {
		return nil, err
	}
	options, err = copyOptions(options)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	store, err := driver.OpenMaintenance(ctx, options)
	if err != nil {
		if !missing(store) {
			_ = store.Close()
		}
		return nil, err
	}
	if missing(store) {
		return nil, domain.Fail("TEMPORARY_FAILURE", "Storage backend returned no maintenance store")
	}
	return store, nil
}
