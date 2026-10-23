package e2e_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestPlaylistsActualServerLocalRemoteHiddenMatchingAndRestart(t *testing.T) {
	provider := build(t, "musicfs-plugin-local")
	program := filepath.Join(t.TempDir(), "playlist-fixture")
	if runtime.GOOS == "windows" {
		program += ".exe"
	}
	command := exec.Command("go", "build", "-mod=readonly", "-o", program, "./tests/testkit/playlist-worker")
	command.Dir = testkit.RepositoryRoot()
	if raw, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build playlist worker: %v %s", err, raw)
	}
	wave, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	music := t.TempDir()
	for _, name := range []string{"a.wav", "b.wav", "c.wav"} {
		if err := os.WriteFile(filepath.Join(music, name), wave, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.plugin.source.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Synthetic playlist", Path: music}}}}}}, onlinePlugin(online.Netease, program, "7fe223d0-a3bd-5e87-87f8-305dc26d9041")}
	disableLocalLyricsImport(&cfg)
	var parent playlists.Playlist
	var first playlists.AggregatePage
	var sourceIDs []string
	var ruleID string
	complete := false
	t.Run("compose-and-confirm-hidden-intent", func(t *testing.T) {
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		accountID := authenticatePlaylistFixture(t, c)
		tracks := scanNCM(t, c)
		for _, name := range []string{"a.wav", "b.wav", "c.wav"} {
			sourceIDs = append(sourceIDs, tracks[name].ID)
		}
		local := func(ids ...string) playlists.PlaylistBinding {
			b := playlists.PlaylistBinding{Kind: "local", Entries: []playlists.LocalEntry{}}
			for _, id := range ids {
				b.Entries = append(b.Entries, playlists.LocalEntry{Source: playlists.SourceRef{SourceTrackID: &id}})
			}
			return b
		}
		remote := albumRequest[remoteImported](t, c, "POST", "/remote-playlists", playlists.RemoteRef{InstanceID: "7fe223d0-a3bd-5e87-87f8-305dc26d9041", AccountID: accountID, ExternalID: "91"}, "aggregate-import", 201, "RemotePlaylistImported").Playlist
		accepted := albumRequest[remoteAccepted](t, c, "POST", "/remote-playlists/"+remote.ID+"/sync-jobs", map[string]int{"expectedBindingRevision": 1}, "aggregate-remote-sync", 202, "HashAccepted")
		deadline := time.Now().Add(8 * time.Second)
		for {
			job := albumRequest[playlists.SyncJob](t, c, "GET", "/remote-playlist-sync-jobs/"+accepted.JobID, nil, "", 200, "RemotePlaylistSyncJob")
			if job.State == "succeeded" {
				break
			}
			if job.State == "failed" || time.Now().After(deadline) {
				t.Fatalf("remote sync: %+v", job)
			}
			time.Sleep(30 * time.Millisecond)
		}
		child := albumRequest[playlists.Playlist](t, c, "POST", "/playlists", playlists.CreatePlaylistRequest{Name: "Child", Bindings: []playlists.PlaylistBinding{local(sourceIDs[0], sourceIDs[1], sourceIDs[0])}}, "aggregate-child", 201, "Playlist")
		parent = albumRequest[playlists.Playlist](t, c, "POST", "/playlists", playlists.CreatePlaylistRequest{Name: "Parent", Bindings: []playlists.PlaylistBinding{{Kind: "playlist", TargetID: &child.ID}, local(sourceIDs[2]), {Kind: "remote", TargetID: &remote.ID}}}, "aggregate-parent", 201, "Playlist")
		path := "/playlists/" + parent.ID
		build := func(key string, ruleRevision int64) playlists.AggregatePage {
			t.Helper()
			receipt := albumRequest[playlists.AggregateSnapshot](t, c, "POST", path+"/rebuild", map[string]int64{"expectedRuleRevision": ruleRevision}, key, 200, "PlaylistSnapshotReceipt")
			return albumRequest[playlists.AggregatePage](t, c, "GET", path+"/snapshots/"+strconv.FormatInt(receipt.Revision, 10), nil, "", 200, "PlaylistSnapshotPage")
		}
		first = build("aggregate-first", 1)
		if first.Total != 5 || len(first.Items[0].Provenance) != 2 || first.Items[3].Source.RemoteSourceID == nil || len(first.Items[3].Provenance) != 2 {
			t.Fatal("local and remote provenance were not retained")
		}
		hidden := albumRequest[playlists.HiddenRules](t, c, "POST", path+"/hidden-rules", map[string]any{"expectedRevision": 0, "source": playlists.SourceRef{SourceTrackID: &sourceIDs[0]}}, "aggregate-hide-anchor", 200, "PlaylistHiddenRules")
		ruleID = hidden.Items[0].ID
		if build("aggregate-hidden", 1).Total != 4 {
			t.Fatal("hide affected unrelated contributions")
		}
		linked := albumRequest[matching.Mutation](t, c, "POST", "/matching/links", matching.LinkRequest{SourceTrackIDs: sourceIDs[:2]}, "aggregate-link", 200, "MatchingMutation")
		expanded := build("aggregate-expanded", 1)
		if expanded.Total != 4 || len(expanded.NeedsConfirmation) != 1 || *expanded.Items[0].Source.SourceTrackID != sourceIDs[1] {
			t.Fatal("match expanded hidden intent without confirmation")
		}
		albumRequest[playlists.HiddenRules](t, c, "PUT", path+"/hidden-rules/"+ruleID, playlists.ChangeHiddenRequest{ExpectedRevision: 1, Active: true, Confirm: true}, "aggregate-confirm", 200, "PlaylistHiddenRules")
		if build("aggregate-confirmed", 1).Total != 3 {
			t.Fatal("explicit confirmation did not expand hide")
		}
		albumRequest[matching.Mutation](t, c, "POST", "/matching/unlinks", matching.UnlinkRequest{ExpectedRevision: linked.Revision, SourceTrackID: sourceIDs[0]}, "aggregate-unlink", 200, "MatchingMutation")
		if build("aggregate-split", 1).Total != 4 {
			t.Fatal("split propagated hide to another source")
		}
		parent = albumRequest[playlists.Playlist](t, c, "PUT", path, playlists.EditPlaylistRequest{ExpectedRevision: 1, Name: parent.Name, Bindings: parent.Bindings[1:]}, "aggregate-remove-child", 200, "Playlist")
		if final := build("aggregate-final", 2); final.Total != 3 || len(final.Items[0].Provenance) != 1 || final.Items[1].Source.RemoteSourceID == nil {
			t.Fatal("removed binding erased another contribution")
		}
		old := albumRequest[playlists.AggregatePage](t, c, "GET", path+"/snapshots/"+strconv.FormatInt(first.Revision, 10), nil, "", 200, "PlaylistSnapshotPage")
		if old.Total != 5 || old.Items[0].ID != first.Items[0].ID {
			t.Fatal("historical snapshot changed")
		}
		complete = true
	})
	t.Run("restart-retains-history-and-local-playback", func(t *testing.T) {
		if !complete {
			t.Skip("initial phase failed")
		}
		cfg.Plugins[1].Enabled = false
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		path := "/playlists/" + parent.ID
		restored := albumRequest[playlists.Playlist](t, c, "GET", path, nil, "", 200, "Playlist")
		if restored.RuleRevision != 2 || restored.HideRevision != 2 {
			t.Fatal("independent revisions lost on restart")
		}
		hidden := albumRequest[playlists.HiddenRules](t, c, "GET", path+"/hidden-rules", nil, "", 200, "PlaylistHiddenRules")
		if len(hidden.Items) != 1 || hidden.Items[0].ID != ruleID || !hidden.Items[0].Active {
			t.Fatal("hidden intention lost on restart")
		}
		old := albumRequest[playlists.AggregatePage](t, c, "GET", path+"/snapshots/"+strconv.FormatInt(first.Revision, 10), nil, "", 200, "PlaylistSnapshotPage")
		if old.Total != 5 || old.Items[3].Source.RemoteSourceID == nil {
			t.Fatal("disabled remote erased aggregate history")
		}
		description := albumRequest[playback.Description](t, c, "POST", "/playback/sessions", playback.CreateRequest{SourceTrackID: sourceIDs[0], Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}}}}, "aggregate-playback", 201, "Playback")
		verifyRanges(t, c, description, wave)
		if code, _, _ := c.request(t, "DELETE", "/api/v1/playback/sessions/"+description.SessionID, nil, ""); code != 204 {
			t.Fatal("playback stop failed")
		}
		for _, name := range []string{"a.wav", "b.wav", "c.wav"} {
			raw, err := os.ReadFile(filepath.Join(music, name))
			if err != nil || !bytes.Equal(raw, wave) {
				t.Fatal("playlist actions changed source bytes")
			}
		}
	})
}
