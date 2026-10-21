package storage_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

type trackedBackend struct {
	storage.Backend
	closed int
}

func (s *trackedBackend) Close() error {
	s.closed++
	if s.Backend != nil {
		return s.Backend.Close()
	}
	return nil
}

func TestRegisteredBackendOwnsRuntimeAndMaintenance(t *testing.T) {
	ctx := context.Background()
	registry := storage.NewRegistry()
	driver := sqlite.Driver()
	driver.Name = "test-storage"
	originalOpen := driver.Open
	var opened *trackedBackend
	driver.Open = func(ctx context.Context, options storage.Options) (storage.Backend, error) {
		store, err := originalOpen(ctx, options)
		opened = &trackedBackend{Backend: store}
		return opened, err
	}
	if err := registry.Register(driver); err != nil {
		t.Fatal(err)
	}
	options := storage.Options{DataRoot: t.TempDir()}
	store, err := registry.Open(ctx, "test-storage", options)
	if err != nil {
		t.Fatal(err)
	}
	session := auth.Session{ID: "test-session", User: auth.User{ID: "admin", Role: "admin"}, CSRFToken: "test-csrf", ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if err = store.SaveAuth(ctx, session); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil || opened.closed != 1 {
		t.Fatalf("close: %v, calls: %d", err, opened.closed)
	}
	store, err = registry.Open(ctx, "test-storage", options)
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.FindAuth(ctx, session.ID)
	if err != nil || got.User != session.User || got.CSRFToken != session.CSRFToken {
		t.Fatalf("persisted session: %+v, %v", got, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	maintenance, err := registry.OpenMaintenance(ctx, "test-storage", options)
	if err != nil {
		t.Fatal(err)
	}
	size, err := maintenance.DatabaseSize(ctx)
	if err != nil || size <= 0 {
		t.Fatalf("database size: %d, %v", size, err)
	}
	if err = maintenance.PrepareRestoredDatabase(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err = maintenance.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = registry.Open(ctx, "test-storage", options)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err = store.FindAuth(ctx, session.ID); err == nil {
		t.Fatal("restored authentication must be revoked")
	}
}

func TestUnknownBackendNeverFallsBackOrCreatesDatabase(t *testing.T) {
	root := filepath.Join(t.TempDir(), "untouched")
	if _, err := storage.Open(context.Background(), "unknown-backend", storage.Options{DataRoot: root}); err == nil {
		t.Fatal("unknown backend was accepted")
	}
	if _, err := storage.OpenMaintenance(context.Background(), "unknown-backend", storage.Options{DataRoot: root}); err == nil {
		t.Fatal("unknown maintenance backend was accepted")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("unknown backend touched data root: %v", err)
	}
}

func TestDefaultSQLiteAndRegistrationValidation(t *testing.T) {
	driver, err := storage.Lookup("")
	if err != nil || driver.Name != "sqlite" {
		t.Fatalf("default driver: %+v, %v", driver, err)
	}
	registry := storage.NewRegistry()
	if err = registry.Register(driver); err != nil {
		t.Fatal(err)
	}
	if err = registry.Register(driver); err == nil {
		t.Fatal("duplicate registration replaced the active driver")
	}
	for _, change := range []func(*storage.Driver){
		func(d *storage.Driver) { d.Name = "../bad" },
		func(d *storage.Driver) { d.Version++ },
		func(d *storage.Driver) { d.PluginVersion = "" },
		func(d *storage.Driver) { d.PluginVersion = " " },
		func(d *storage.Driver) { d.PluginVersion = "1.0" },
		func(d *storage.Driver) { d.PluginVersion = "1.0.0-01" },
		func(d *storage.Driver) { d.Open = nil },
		func(d *storage.Driver) { d.OpenMaintenance = nil },
		func(d *storage.Driver) { d.InspectBackup = nil },
		func(d *storage.Driver) { d.BackupSchemaVersion = nil },
		func(d *storage.Driver) { d.SnapshotName = "../db.sqlite" },
		func(d *storage.Driver) { d.RestoredName = "../musicfs.sqlite" },
	} {
		bad := sqlite.Driver()
		change(&bad)
		if err := storage.NewRegistry().Register(bad); err == nil {
			t.Fatalf("invalid registration accepted: %+v", bad)
		}
	}
}

func TestFailedFactoryClosesPartialBackendAndRejectsNil(t *testing.T) {
	for _, typedNil := range []bool{false, true} {
		registry := storage.NewRegistry()
		driver := sqlite.Driver()
		driver.Open = func(context.Context, storage.Options) (storage.Backend, error) {
			if typedNil {
				var store *trackedBackend
				return store, nil
			}
			return nil, nil
		}
		if err := registry.Register(driver); err != nil {
			t.Fatal(err)
		}
		if _, err := registry.Open(context.Background(), "", storage.Options{DataRoot: t.TempDir()}); err == nil {
			t.Fatal("empty factory result accepted")
		}
	}
	registry := storage.NewRegistry()
	driver := sqlite.Driver()
	partial := &trackedBackend{}
	want := errors.New("incomplete initialization")
	driver.Open = func(context.Context, storage.Options) (storage.Backend, error) { return partial, want }
	if err := registry.Register(driver); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Open(context.Background(), "", storage.Options{DataRoot: t.TempDir()}); !errors.Is(err, want) || partial.closed != 1 {
		t.Fatalf("partial initialization: %v, closes: %d", err, partial.closed)
	}
}

func TestFactoryOptionsAreIsolatedAndCancellationPreventsOpening(t *testing.T) {
	registry := storage.NewRegistry()
	driver := sqlite.Driver()
	calls := 0
	driver.Open = func(_ context.Context, options storage.Options) (storage.Backend, error) {
		calls++
		options.Values["custom"] = "changed"
		options.Args[0] = "changed"
		return &trackedBackend{}, nil
	}
	if err := registry.Register(driver); err != nil {
		t.Fatal(err)
	}
	options := storage.Options{DataRoot: t.TempDir(), Values: map[string]string{"custom": "original"}, Args: []string{"original"}}
	store, err := registry.Open(context.Background(), "", options)
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	if options.Values["custom"] != "original" || options.Args[0] != "original" {
		t.Fatal("factory changed caller configuration")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = registry.Open(ctx, "", options); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("canceled factory: %v, calls: %d", err, calls)
	}
	if _, err = registry.Open(context.Background(), "", storage.Options{DataRoot: "relative"}); err == nil || calls != 1 {
		t.Fatalf("relative data root: %v, calls: %d", err, calls)
	}
}

func TestSQLiteRejectsUnknownBackendOptions(t *testing.T) {
	for _, options := range []storage.Options{
		{Values: map[string]string{"unsafe": "ignored"}},
		{Executable: "unused"},
		{Args: []string{"unused"}},
		{SHA256: "unused"},
	} {
		options.DataRoot = t.TempDir()
		_, err := storage.Open(context.Background(), "", options)
		var detail *domain.Error
		if !errors.As(err, &detail) || detail.Code != "INVALID_ARGUMENT" {
			t.Fatalf("unexpected backend options: %v", err)
		}
	}
}
