package plugincontrol_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
)

const document = `configVersion: 1
listen: '127.0.0.1:8080'
allowLoopbackHttp: true
allowedOrigins: ['http://127.0.0.1:8080']
plugins:
  - pluginId: musicfs.ncm
    executable: './installed-worker'
    enabled: true
    instances:
      - instanceId: ncm
        name: Decoder
        role: audio
        enabled: true
        config: {}
`

type harness struct {
	manager       *plugincontrol.Manager
	store         *config.Store
	repo          plugincontrol.Repository
	path          string
	ready         atomic.Bool
	starts, stops atomic.Int32
}

func setup(t *testing.T, wrap ...func(plugincontrol.Repository) plugincontrol.Repository) *harness {
	t.Helper()
	return setupDocument(t, document, wrap...)
}

func setupDocument(t *testing.T, raw string, wrap ...func(plugincontrol.Repository) plugincontrol.Repository) *harness {
	t.Helper()
	t.Setenv("MUSICFS_ADMIN_TOKEN", "test-only-long-administrator-token")
	dir := t.TempDir()
	path := filepath.Join(dir, "server.yaml")
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	repo, err := sqlite.Open(context.Background(), filepath.Join(dir, "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	store, err := config.OpenStore(context.Background(), path, nil, repo)
	if err != nil {
		t.Fatal(err)
	}
	var operationRepo plugincontrol.Repository = repo
	for _, fn := range wrap {
		operationRepo = fn(operationRepo)
	}
	manager, err := plugincontrol.New(context.Background(), store.Current(), store, operationRepo)
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{manager: manager, store: store, repo: operationRepo, path: path}
	initial := store.Current().Plugins[0]
	enabled := initial.CanStart(initial.Instances[0])
	h.ready.Store(enabled)
	if err = manager.Register("ncm", plugincontrol.Hooks{
		Ready: h.ready.Load,
		Start: func(context.Context, config.Plugin, config.Instance) error {
			h.starts.Add(1)
			h.ready.Store(true)
			return nil
		},
		Stop: func(ctx context.Context) error {
			for manager.Gate("ncm").Active() > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Millisecond):
				}
			}
			h.stops.Add(1)
			h.ready.Store(false)
			return nil
		},
	}, enabled); err != nil {
		t.Fatal(err)
	}
	manager.Start()
	t.Cleanup(manager.Close)
	return h
}
func change(t *testing.T, h *harness, key string, enabled bool, mode string) plugincontrol.Operation {
	t.Helper()
	op, err := h.manager.Change(context.Background(), "admin", config.Change{ExpectedRevision: h.manager.Snapshot().Revision, IdempotencyKey: key, PluginID: "musicfs.ncm", Enabled: enabled, Mode: mode})
	if err != nil {
		t.Fatal(err)
	}
	return op
}
func waitStatus(t *testing.T, h *harness, op plugincontrol.Operation, status string) {
	t.Helper()
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		v, err := h.manager.Operation(context.Background(), "admin", op.ID)
		if err == nil && v.Status == status {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	v, _ := h.manager.Operation(context.Background(), "admin", op.ID)
	t.Fatalf("operation state %s, wanted %s", v.Status, status)
}
func code(t *testing.T, err error, want string) {
	t.Helper()
	var d *domain.Error
	if !errors.As(err, &d) || d.Code != want {
		t.Fatalf("wanted %s, got %v", want, err)
	}
}

func TestPluginManagerIdempotencyRevisionSettingsAndEvents(t *testing.T) {
	h := setup(t)
	updates, closeUpdates := h.manager.Subscribe()
	defer closeUpdates()
	<-updates
	input := config.Change{ExpectedRevision: h.manager.Snapshot().Revision, IdempotencyKey: "save-name", PluginID: "musicfs.ncm", InstanceID: "ncm", Settings: &config.InstanceSettings{}}
	name := "Renamed"
	input.Settings.Name = &name
	op, err := h.manager.Change(context.Background(), "admin", input)
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, h, op, "succeeded")
	again, err := h.manager.Change(context.Background(), "admin", input)
	if err != nil || again.ID != op.ID {
		t.Fatal("same request not replayed", err)
	}
	select {
	case event := <-updates:
		if event.Revision == "" {
			t.Fatal("event missing revision")
		}
	case <-time.After(time.Second):
		t.Fatal("state update not broadcast")
	}
	otherName := "Conflicting name"
	input.Settings = &config.InstanceSettings{Name: &otherName}
	_, err = h.manager.Change(context.Background(), "admin", input)
	code(t, err, "CONFLICT")
	input.IdempotencyKey = "stale-key"
	_, err = h.manager.Change(context.Background(), "admin", input)
	code(t, err, "PRECONDITION_FAILED")
	view, err := h.manager.Configuration("ncm")
	if err != nil || view.Name != name {
		t.Fatal("configuration not applied", err)
	}
}

func TestPluginGateCancelIsNotResourceRelease(t *testing.T) {
	gate := plugincontrol.NewGate(true)
	lease, err := gate.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gate.SetAccepting(false, true)
	if gate.Active() != 1 || lease.Context().Err() == nil {
		t.Fatal("force discarded cleanup lease")
	}
	_, err = gate.Acquire(lease.Context())
	if err == nil {
		t.Fatal("canceled scope was reused")
	}
	lease.Close()
	lease.Close()
	if gate.Active() != 0 {
		t.Fatal("lease leaked")
	}
}
