package e2e_test

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type remoteImported struct {
	JobID    string                   `json:"jobId"`
	Playlist playlists.RemotePlaylist `json:"playlist"`
	Merged   bool                     `json:"merged"`
}
type remoteAccepted struct {
	JobID  string `json:"jobId"`
	Merged bool   `json:"merged"`
}

func authenticatePlaylistFixture(t *testing.T, c *client) string {
	t.Helper()
	authenticateManagedOnline(t, c, "7fe223d0-a3bd-5e87-87f8-305dc26d9041")
	code, raw, _ := c.request(t, "GET", "/api/v1/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/account", nil, "")
	var account online.AccountView
	if code != 200 || json.Unmarshal(raw, &account) != nil || account.State != "signedIn" || account.AccountID == "" {
		t.Fatalf("playlist fixture account was not authenticated: %d %s", code, raw)
	}
	return account.AccountID
}

func TestRemotePlaylistsActualServerConservativeSyncAndDisabledRestart(t *testing.T) {
	program := filepath.Join(t.TempDir(), "playlist-fixture")
	if runtime.GOOS == "windows" {
		program += ".exe"
	}
	command := exec.Command("go", "build", "-mod=readonly", "-o", program, "./tests/testkit/playlist-worker")
	command.Dir = testkit.RepositoryRoot()
	if raw, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build playlist fixture: %v %s", err, raw)
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{onlinePlugin(online.Netease, program, "7fe223d0-a3bd-5e87-87f8-305dc26d9041")}
	var imported playlists.RemotePlaylist
	var first playlists.SnapshotPage
	t.Run("explicit-import-and-conservative-sync", func(t *testing.T) {
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		accountID := authenticatePlaylistFixture(t, c)
		ref := playlists.RemoteRef{InstanceID: "7fe223d0-a3bd-5e87-87f8-305dc26d9041", AccountID: accountID, ExternalID: "91"}
		created := albumRequest[remoteImported](t, c, "POST", "/remote-playlists", ref, "import-first", 201, "RemotePlaylistImported")
		imported = created.Playlist
		if imported.Revision != 0 || imported.Title != "" || !imported.Stale || imported.LastSuccessAt != nil {
			t.Fatal("import unexpectedly fetched upstream")
		}
		path := "/remote-playlists/" + imported.ID
		wait := func(id, state string) playlists.SyncJob {
			t.Helper()
			deadline := time.Now().Add(8 * time.Second)
			for time.Now().Before(deadline) {
				job := albumRequest[playlists.SyncJob](t, c, "GET", "/remote-playlist-sync-jobs/"+id, nil, "", 200, "RemotePlaylistSyncJob")
				if job.State == state {
					return job
				}
				if job.State == "failed" || job.State == "cancelled" || job.State == "succeeded" {
					t.Fatalf("unexpected sync state %+v", job)
				}
				time.Sleep(30 * time.Millisecond)
			}
			t.Fatal("sync did not finish")
			return playlists.SyncJob{}
		}
		sync := func(key, state string) playlists.SyncJob {
			t.Helper()
			accepted := albumRequest[remoteAccepted](t, c, "POST", path+"/sync-jobs", map[string]int{"expectedBindingRevision": 1}, key, 202, "HashAccepted")
			return wait(accepted.JobID, state)
		}
		read := func(revision int64) playlists.SnapshotPage {
			t.Helper()
			return albumRequest[playlists.SnapshotPage](t, c, "GET", path+"/snapshots/"+strconv.FormatInt(revision, 10), nil, "", 200, "RemotePlaylistSnapshotPage")
		}
		job := wait(created.JobID, "succeeded")
		if job.ResultRevision == nil {
			t.Fatal("missing snapshot result")
		}
		first = read(*job.ResultRevision)
		if first.Total != 3 || first.Items[0].ID == first.Items[1].ID || first.Items[0].Source.ID != first.Items[1].Source.ID || first.Items[0].Source.PlaybackAvailability != "unsupported" {
			t.Fatal("duplicate positions or remote source identity changed")
		}
		time.Sleep(1100 * time.Millisecond)
		failed := sync("second-failure", "failed")
		if failed.Error == nil || failed.Error.Code != "TEMPORARY_FAILURE" {
			t.Fatalf("failed observation lost: %+v", failed)
		}
		stale := albumRequest[playlists.RemotePlaylist](t, c, "GET", path, nil, "", 200, "RemotePlaylist")
		if !stale.Stale || stale.Revision != 1 || stale.LastSuccessAt == nil || stale.Error == nil {
			t.Fatal("failure cleared old successful snapshot")
		}
		if old := read(1); old.Items[1].ID != first.Items[1].ID || old.Total != 3 {
			t.Fatal("failure changed snapshot")
		}
		time.Sleep(1100 * time.Millisecond)
		job = sync("first-missing", "succeeded")
		suspect := read(*job.ResultRevision)
		missing := 0
		for _, entry := range suspect.Items {
			if entry.State == "suspectedRemoved" {
				missing++
				if entry.FirstMissingObservation == nil || entry.ID != first.Items[1].ID {
					t.Fatal("missing occurrence identity changed")
				}
			}
		}
		if missing != 1 || suspect.Total != 3 {
			t.Fatal("unversioned absence deleted immediately")
		}
		time.Sleep(1100 * time.Millisecond)
		job = sync("confirmed-missing", "succeeded")
		confirmed := read(*job.ResultRevision)
		if confirmed.Total != 2 || confirmed.Items[0].ID != first.Items[0].ID || confirmed.Items[1].ID != first.Items[2].ID {
			t.Fatal("later full observation did not conservatively reconcile")
		}
		if old := read(1); old.Total != 3 || old.Items[1].ID != first.Items[1].ID {
			t.Fatal("old local revision changed")
		}
		time.Sleep(1100 * time.Millisecond)
		slow := albumRequest[remoteImported](t, c, "POST", "/remote-playlists", playlists.RemoteRef{InstanceID: "7fe223d0-a3bd-5e87-87f8-305dc26d9041", AccountID: accountID, ExternalID: "92"}, "import-cancelled", 201, "RemotePlaylistImported")
		accepted := albumRequest[remoteAccepted](t, c, "POST", "/remote-playlists/"+slow.Playlist.ID+"/sync-jobs", map[string]int{"expectedBindingRevision": 1}, "cancelled-sync", 202, "HashAccepted")
		_ = wait(accepted.JobID, "running")
		cancelled := albumRequest[playlists.SyncJob](t, c, "POST", "/remote-playlist-sync-jobs/"+accepted.JobID+"/cancel", struct{}{}, "", 200, "RemotePlaylistSyncJob")
		if cancelled.State != "cancelled" {
			t.Fatal("active sync did not cancel")
		}
		assertImportedPlaylistTracks(t, c)
	})
	t.Run("disabled-restart-keeps-private-history", func(t *testing.T) {
		if imported.ID == "" || len(first.Items) == 0 {
			t.Skip("initial phase failed")
		}
		cfg.Plugins[0].Enabled = false
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		path := "/remote-playlists/" + imported.ID
		current := albumRequest[playlists.RemotePlaylist](t, c, "GET", path, nil, "", 200, "RemotePlaylist")
		if current.ID != imported.ID || current.Revision != 3 {
			t.Fatal("restart lost remote playlist revisions")
		}
		old := albumRequest[playlists.SnapshotPage](t, c, "GET", path+"/snapshots/1", nil, "", 200, "RemotePlaylistSnapshotPage")
		if old.Items[1].ID != first.Items[1].ID || old.Total != 3 {
			t.Fatal("restart changed historical occurrence")
		}
		albumRequest[map[string]any](t, c, "POST", path+"/sync-jobs", map[string]int{"expectedBindingRevision": 1}, "disabled-sync", 503, "Error")
		assertImportedPlaylistTracks(t, c)
	})
}

func assertImportedPlaylistTracks(t *testing.T, c *client) {
	t.Helper()
	code, raw, _ := c.request(t, "GET", "/api/v1/source-tracks", nil, "")
	var page struct {
		Items []json.RawMessage `json:"items"`
	}
	if code != 200 || json.Unmarshal(raw, &page) != nil || len(page.Items) != 2 {
		t.Fatalf("expected two imported source tracks: %d %s", code, raw)
	}
}
