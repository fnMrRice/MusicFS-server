package e2e_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/playlists"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestRecoveredOnlineTaskAcquiresGatesBeforeFirstObservation(t *testing.T) {
	program := filepath.Join(t.TempDir(), "playlist-recovery")
	if runtime.GOOS == "windows" {
		program += ".exe"
	}
	command := exec.Command("go", "build", "-mod=readonly", "-o", program, "./tests/testkit/authenticated-playlist-worker")
	command.Dir = testkit.RepositoryRoot()
	if raw, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build playlist recovery worker: %v %s", err, raw)
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{onlinePlugin("netease", program, "7fe223d0-a3bd-5e87-87f8-305dc26d9041")}
	if err := os.MkdirAll(cfg.DataRoot, 0700); err != nil {
		t.Fatal(err)
	}
	// Persist a running ticket as left by an interrupted server. The restarted
	// service must recover it with a new generation and full task admission.
	store, err := sqlite.Open(context.Background(), filepath.Join(cfg.DataRoot, "musicfs.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(program)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	binding, _ := json.Marshal(struct{ Plugin, Instance, Binary, Schema string }{"musicfs.plugin.source.netease", "7fe223d0-a3bd-5e87-87f8-305dc26d9041", fmt.Sprintf("%x", sha256.Sum256(binary)), pluginproto.OnlineRevision})
	ref := playlists.Binding{RemoteRef: playlists.RemoteRef{InstanceID: "7fe223d0-a3bd-5e87-87f8-305dc26d9041", AccountID: "fixture-account", ExternalID: "92"}, Platform: "netease", ConfigRevision: fmt.Sprintf("%x", sha256.Sum256(binding))}
	playlist, _, err := store.ImportRemotePlaylist(context.Background(), "admin", ref, "recovery-import")
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	queued, _, err := store.QueueRemoteSync(context.Background(), "admin", playlist.ID, 1, ref, "recovery-sync")
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	interrupted, err := store.ClaimRemoteSync(context.Background())
	store.Close()
	if err != nil || interrupted.ID != queued.ID || interrupted.State != "running" {
		t.Fatal("failed to persist interrupted synchronization", interrupted, err)
	}
	path := writeYAMLConfiguration(t, cfg)
	stop := startAudioYAMLServer(t, path, cfg, base)
	defer stop()
	c := login(t, base, cfg.AdminToken)
	var recovered playlists.SyncJob
	for deadline := time.Now().Add(3 * time.Second); ; {
		code, raw, _ := c.request(t, "GET", "/api/v1/remote-playlist-sync-jobs/"+queued.ID, nil, "")
		if code != 200 || json.Unmarshal(raw, &recovered) != nil {
			t.Fatalf("recovered synchronization unavailable: %d %s", code, raw)
		}
		if recovered.State == "running" && recovered.Generation > interrupted.Generation {
			break
		}
		if recovered.State == "failed" || time.Now().After(deadline) {
			t.Fatal("persisted task was not resumed", recovered)
		}
		time.Sleep(10 * time.Millisecond)
	}
	state := managedOnlineInstance(t, c, "7fe223d0-a3bd-5e87-87f8-305dc26d9041")
	if state.ActiveLeases < 2 {
		t.Fatal("recovery began before task and RPC gates were attached", state)
	}
	operation := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/state", map[string]any{"enabled": false, "mode": "force"})
	waitLibraryOperation(t, c, operation.ID)
	time.Sleep(150 * time.Millisecond)
	generation := recovered.Generation
	code, raw, _ := c.request(t, "GET", "/api/v1/remote-playlist-sync-jobs/"+queued.ID, nil, "")
	if code != 200 || json.Unmarshal(raw, &recovered) != nil || recovered.State != "running" || recovered.Generation != generation {
		t.Fatalf("pending disable interrupted the recovered task: %d %s", code, raw)
	}
	if state = managedOnlineInstance(t, c, "7fe223d0-a3bd-5e87-87f8-305dc26d9041"); state.ActiveLeases < 2 || state.RuntimeState != "ready" || !state.AcceptingNewWork || state.Reason == nil || *state.Reason != "RESTART_REQUIRED" {
		t.Fatal("pending disable changed recovered task admission", state)
	}
}
