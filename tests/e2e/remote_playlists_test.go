package e2e_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/playlists"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type remoteImported struct {
	Playlist playlists.RemotePlaylist `json:"playlist"`
	Merged   bool                     `json:"merged"`
}
type remoteAccepted struct {
	JobID  string `json:"jobId"`
	Merged bool   `json:"merged"`
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
	cfg.Plugins = []config.Plugin{onlinePlugin(online.Netease, program, "net")}
	var imported playlists.RemotePlaylist
	var first playlists.SnapshotPage
	t.Run("explicit-import-and-conservative-sync", func(t *testing.T) {
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		ref := playlists.RemoteRef{InstanceID: "net", ExternalID: "91"}
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
		job := sync("first-success", "succeeded")
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
		slow := albumRequest[remoteImported](t, c, "POST", "/remote-playlists", playlists.RemoteRef{InstanceID: "net", ExternalID: "92"}, "import-cancelled", 201, "RemotePlaylistImported")
		accepted := albumRequest[remoteAccepted](t, c, "POST", "/remote-playlists/"+slow.Playlist.ID+"/sync-jobs", map[string]int{"expectedBindingRevision": 1}, "cancelled-sync", 202, "HashAccepted")
		_ = wait(accepted.JobID, "running")
		cancelled := albumRequest[playlists.SyncJob](t, c, "POST", "/remote-playlist-sync-jobs/"+accepted.JobID+"/cancel", struct{}{}, "", 200, "RemotePlaylistSyncJob")
		if cancelled.State != "cancelled" {
			t.Fatal("active sync did not cancel")
		}
		assertOnlineDoesNotImport(t, c)
	})
	t.Run("disabled-restart-keeps-private-history", func(t *testing.T) {
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
		assertOnlineDoesNotImport(t, c)
	})
}
