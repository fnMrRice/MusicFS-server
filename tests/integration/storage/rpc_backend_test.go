package storage_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugindb"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginrpc"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	_ "github.com/fnMrRice/MusicFS-server/src/storage/rpcbackend"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func storageWorker(t *testing.T, packages ...string) storage.Options {
	t.Helper()
	path := filepath.Join(t.TempDir(), "storage-worker")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	pkg := "./plugins/database/sqlite/cmd/musicfs-plugin-storage-sqlite"
	if len(packages) > 0 {
		pkg = packages[0]
	}
	cmd := exec.Command("go", "build", "-o", path, pkg)
	cmd.Dir = filepath.Join("..", "..", "..")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build persistence worker: %v %s", err, output)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return storage.Options{Executable: path, SHA256: hex.EncodeToString(sum[:])}
}

func requireStorageAvailable(t *testing.T, store storage.Backend, want bool) {
	t.Helper()
	version, ok := store.(interface{ PluginVersion() string })
	if !ok || version.PluginVersion() != bundledplugin.SQLiteVersion {
		t.Fatal("persistence worker did not retain its verified release")
	}
	status, ok := store.(storage.Availability)
	if !ok {
		t.Fatal("persistence worker does not report availability")
	}
	if got := status.Available(); got != want {
		t.Fatalf("persistence worker availability = %v, want %v", got, want)
	}
}

func TestPersistenceWorkerCrashAfterCommitReportsUnknownOutcome(t *testing.T) {
	options := storageWorker(t, "./tests/testkit/persistence-crash-worker")
	options.DataRoot = t.TempDir()
	ctx := context.Background()
	store, err := storage.Open(ctx, "plugin", options)
	if err != nil {
		t.Fatal(err)
	}
	requireStorageAvailable(t, store, true)
	session := auth.Session{ID: "committed-before-crash", User: auth.User{ID: "admin", Role: "admin"}, ExpiresAt: time.Now().Add(time.Hour)}
	err = store.SaveAuth(ctx, session)
	matchingError(t, err, "TEMPORARY_FAILURE")
	if !strings.Contains(err.Error(), "verify") {
		t.Fatalf("uncertain commit not reported: %v", err)
	}
	requireStorageAvailable(t, store, false)
	if _, err = store.FindAuth(ctx, session.ID); err == nil {
		t.Fatal("crashed worker was silently restarted")
	}
	if state := store.ProviderScanState(ctx, "admin", "source"); state != "failed" {
		t.Fatalf("crashed storage returned a valid-looking scan state: %q", state)
	}
	store.Close()
	requireStorageAvailable(t, store, false)
	local, err := sqlite.Open(ctx, filepath.Join(options.DataRoot, "musicfs.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer local.Close()
	if got, err := local.FindAuth(ctx, session.ID); err != nil || got.ID != session.ID {
		t.Fatalf("committed outcome was not recoverable: %+v %v", got, err)
	}
}

func remoteFixture(t *testing.T, local *sqlite.Store, options storage.Options) storage.Backend {
	t.Helper()
	var seq int
	var name, path string
	if err := local.DB.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	options.DataRoot = filepath.Dir(path)
	target := filepath.Join(options.DataRoot, "musicfs.sqlite")
	if path != target {
		if err := local.Backup(context.Background(), target); err != nil {
			t.Fatal(err)
		}
	}
	if err := local.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := storage.Open(context.Background(), "plugin", options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestRealPersistenceWorker(t *testing.T) {
	options := storageWorker(t)
	t.Run("large-atomic-publish-and-internal-fields", func(t *testing.T) {
		options := options
		options.DataRoot = t.TempDir()
		startup, cancelStartup := context.WithCancel(context.Background())
		defer cancelStartup()
		store, err := storage.Open(startup, "plugin", options)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		requireStorageAvailable(t, store, true)
		cancelStartup()
		requireStorageAvailable(t, store, true)
		ctx := context.Background()
		if _, err = store.Claim(ctx); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("empty scan queue lost SQL sentinel: %v", err)
		}
		if status, err := store.ScanStatus(ctx, "admin", "rpc-fixture"); err != nil || status.Job != nil {
			t.Fatalf("empty scan status RPC failed: %+v %v", status, err)
		}
		job, _, err := store.Enqueue(ctx, "admin", "rpc-fixture", testkit.ScanPlan("music"), "rpc-large-scan")
		if err != nil {
			t.Fatal(err)
		}
		claimed, err := store.Claim(ctx)
		if err != nil || claimed.Owner != job.Owner || claimed.PlanDigest == "" || claimed.Generation <= 0 || !reflect.DeepEqual(claimed.Plan, job.Plan) {
			t.Fatalf("internal fields lost: %+v %v", claimed, err)
		}
		if err = store.UpdateProgress(ctx, claimed, 200); err != nil {
			t.Fatal("running progress RPC failed", err)
		}
		progress, err := store.GetJob(ctx, "admin", claimed.ID)
		if err != nil || progress.Status != "running" || progress.Complete || progress.Progress.Observed != 200 {
			t.Fatalf("running progress RPC lost state: %+v %v", progress, err)
		}
		status, err := store.ScanStatus(ctx, "admin", "rpc-fixture")
		if err != nil || status.Job == nil || status.Job.ID != claimed.ID || status.Job.Progress.Observed != 200 || status.Job.Generation != claimed.Generation || status.Job.PlanDigest != claimed.PlanDigest {
			t.Fatalf("current scan status RPC lost state: %+v %v", status.Job, err)
		}
		if hidden, err := store.ScanStatus(ctx, "other", "rpc-fixture"); err != nil || hidden.Job != nil {
			t.Fatalf("scan status RPC crossed owners: %+v %v", hidden, err)
		}
		stale := claimed
		stale.Generation++
		if err = store.UpdateProgress(ctx, stale, 201); err == nil {
			t.Fatal("progress RPC accepted stale generation")
		}
		items := make([]library.Observation, 2100)
		for i := range items {
			items[i] = library.Observation{Observation: pluginproto.Observation{
				ExternalID: fmt.Sprintf("track-%d", i), RootID: "music", RelativePath: fmt.Sprintf("%d.wav", i),
				ObservedVersion: "v1", DisplayName: strings.Repeat("x", 600), MIME: "audio/wav", SizeBytes: 100,
			}}
		}
		if err = store.Publish(ctx, claimed, testkit.ScanPublication(claimed, items)); err != nil {
			t.Fatal(err)
		}
		if err = store.Finish(ctx, claimed, len(items), nil); err != nil {
			t.Fatalf("nil error argument failed: %v", err)
		}
		statistics, err := store.RootStatistics(ctx, "admin", "rpc-fixture")
		if err != nil || len(statistics.Roots) != 1 || statistics.Roots[0].RootID != "music" || statistics.Roots[0].TrackCount != 2100 || statistics.Roots[0].SizeBytes != 210000 {
			t.Fatalf("root statistics RPC lost totals: %+v %v", statistics, err)
		}
		if hidden, err := store.RootStatistics(ctx, "other", "rpc-fixture"); err != nil || len(hidden.Roots) != 0 {
			t.Fatalf("root statistics RPC crossed owners: %+v %v", hidden, err)
		}
		if state := store.ProviderScanState(ctx, "admin", "rpc-fixture"); state != "succeeded" {
			t.Fatalf("scan state: %q", state)
		}
		if _, err = store.FindAuth(ctx, "missing"); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("SQL sentinel lost: %v", err)
		}
		page, err := store.ListTracks(ctx, "admin", "rpc-fixture", "", 200)
		if err != nil || len(page.Items) != 200 {
			t.Fatalf("published tracks: %d, %v", len(page.Items), err)
		}
		if err = store.Close(); err != nil {
			t.Fatal(err)
		}
		requireStorageAvailable(t, store, false)
	})
	t.Run("nested-source-guard-and-idempotency", func(t *testing.T) {
		local, _, tracks := matchingFixture(t, 1)
		if err := local.MigratePluginInstanceIDs(context.Background()); err != nil {
			t.Fatal(err)
		}
		_, candidate := enrichmentPreview(t, local, tracks[0])
		store := remoteFixture(t, local, options)
		ctx := context.Background()
		guardCalls := 0
		first, err := store.ApplyEnrichment(ctx, "admin", candidate.CandidateID, *candidate.PreviewObjectID, 0, "rpc-guard-operation", func(publish func() error) error {
			guardCalls++
			return publish()
		})
		if err != nil || first.Revision != 1 || guardCalls != 1 {
			t.Fatalf("guarded transaction: %+v, %d, %v", first, guardCalls, err)
		}
		replay, err := store.ApplyEnrichment(ctx, "admin", candidate.CandidateID, *candidate.PreviewObjectID, 0, "rpc-guard-operation", func(func() error) error {
			t.Error("successful transaction replay re-entered source guard")
			return nil
		})
		if err != nil || !reflect.DeepEqual(first, replay) {
			t.Fatalf("guard replay differs: %+v %v", replay, err)
		}
		_, err = store.ApplyEnrichment(ctx, "admin", candidate.CandidateID, *candidate.PreviewObjectID, 0, "rpc-stale-revision", func(func() error) error {
			t.Error("stale revision entered source guard")
			return nil
		})
		matchingError(t, err, "CONFLICT")
	})
	t.Run("garbage-callback-failure-cancellation-and-retry", func(t *testing.T) {
		local, _, tracks := matchingFixture(t, 1)
		old := time.Now().Add(-48 * time.Hour)
		publication := abandonedMedia(t, local, tracks[0], old)
		if err := local.MigratePluginInstanceIDs(context.Background()); err != nil {
			t.Fatal(err)
		}
		store := remoteFixture(t, local, options)
		ctx := context.Background()
		items, err := store.ClaimMediaGarbage(ctx, time.Now().Add(-24*time.Hour), 1)
		if err != nil || len(items) != 1 || items[0].ObjectID != publication.Object.ID {
			t.Fatalf("claim garbage: %+v %v", items, err)
		}
		canceled, cancel := context.WithCancel(ctx)
		err = store.DeleteMediaGarbage(canceled, items[0].ID, func(mediastore.Garbage) error {
			cancel()
			return context.Canceled
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("garbage cancellation: %v", err)
		}
		again, err := store.ClaimMediaGarbage(ctx, time.Now().Add(-24*time.Hour), 1)
		if err != nil || len(again) != 1 || again[0].ID != items[0].ID {
			t.Fatalf("cancelled deletion committed: %+v %v", again, err)
		}
		calls := 0
		if err = store.DeleteMediaGarbage(ctx, again[0].ID, func(g mediastore.Garbage) error {
			calls++
			if g.ObjectID != publication.Object.ID {
				t.Error("wrong callback object")
			}
			return nil
		}); err != nil || calls != 1 {
			t.Fatalf("garbage retry: %d, %v", calls, err)
		}
		if err = store.DeleteMediaGarbage(ctx, again[0].ID, func(mediastore.Garbage) error {
			t.Error("deleted garbage invoked callback twice")
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("scoped-private-state-and-read-only-startup", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "plugin-config.sqlite")
		cfg := plugindb.BackendConfig{Name: "plugin", Executable: options.Executable, SHA256: options.SHA256}
		ctx := context.Background()
		value, err := plugindb.ReadBackend(ctx, cfg, path, "test-plugin", "first-instance", "settings")
		if err == nil || value.Revision != 0 {
			t.Fatalf("missing private database was accepted: %+v %v", value, err)
		}
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("read-only startup created database: %v", err)
		}
		first, err := plugindb.OpenBackend(ctx, cfg, path, "test-plugin", "first-instance")
		if err != nil {
			t.Fatal(err)
		}
		defer first.Close()
		body := []byte(`{"fixture":true}`)
		value, err = first.CompareAndSet(ctx, pluginproto.StateRequest{Key: "settings", Value: body})
		if err != nil || value.Revision != 1 {
			t.Fatalf("private state write: %+v %v", value, err)
		}
		if _, err = first.CompareAndSet(ctx, pluginproto.StateRequest{Key: "settings", Value: body}); err == nil {
			t.Fatal("stale private state revision accepted")
		}
		second, err := plugindb.OpenBackend(ctx, cfg, path, "test-plugin", "second-instance")
		if err != nil {
			t.Fatal(err)
		}
		defer second.Close()
		if value, err = second.Get(ctx, "settings"); err != nil || value.Revision != 0 {
			t.Fatalf("private state crossed instances: %+v %v", value, err)
		}
		if value, err = first.Get(ctx, "settings"); err != nil || !bytes.Equal(value.Value, body) {
			t.Fatalf("private state changed: %+v %v", value, err)
		}
	})
	t.Run("contract-and-executable-rejected-before-opening-database", func(t *testing.T) {
		root := t.TempDir()
		_, err := pluginrpc.Start(context.Background(), pluginrpc.ProcessConfig{Executable: options.Executable, SHA256: options.SHA256, WorkDir: root}, reflect.TypeFor[plugindb.Backend](), "backend", storage.Options{DataRoot: root})
		matchingError(t, err, "PROTOCOL_MISMATCH")
		if _, err = os.Stat(filepath.Join(root, "musicfs.sqlite")); !os.IsNotExist(err) {
			t.Fatalf("wrong contract opened database: %v", err)
		}
		invalid := options
		invalid.DataRoot, invalid.SHA256 = root, strings.Repeat("0", 64)
		if _, err = storage.Open(context.Background(), "plugin", invalid); err == nil {
			t.Fatal("unverified executable started")
		}
	})
}
