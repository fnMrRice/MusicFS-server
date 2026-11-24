package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestNeteaseImportMatchesNewSourcesOnly(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE to scan tagged local audio")
	}
	const localID = "b1548e96-3081-5c9c-8376-bf290e369d51"
	const instance = "7fe223d0-a3bd-5e87-87f8-305dc26d9041"
	const remote = "/online-instances/" + instance
	directory := t.TempDir()
	audio, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "vbr-untagged.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	for filename, title := range map[string]string{"one.mp3": "New song", "two.mp3": "New song", "older.mp3": "Previous song"} {
		tag := testkit.ScraperID3(4,
			testkit.ScraperFrame{ID: "TIT2", Body: []byte("\x03" + title)},
			testkit.ScraperFrame{ID: "TPE1", Body: []byte("\x03Artist")},
			testkit.ScraperFrame{ID: "TALB", Body: []byte("\x03Album")})
		if err := os.WriteFile(filepath.Join(directory, filename), append(tag, audio...), 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, base := yamlServiceConfig(t)
	netease := onlinePlugin(online.Netease, managedOnlineFixture(t), instance)
	netease.Args = []string{"catalog-match"}
	cfg.Plugins = []config.Plugin{
		{PluginID: "musicfs.plugin.source.localfiles", Executable: build(t, "musicfs-plugin-local"), Enabled: true,
			Instances: []config.Instance{{InstanceID: localID, Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Local", Path: directory}}}}}},
		{PluginID: "musicfs.plugin.decoder.ffmpeg", Executable: build(t, "musicfs-plugin-audio-ffmpeg"), Enabled: true,
			Instances: []config.Instance{{InstanceID: "16b003e5-1bab-52bb-be10-0e83ec1c40ea", Enabled: true, Config: config.InstanceConfig{FFmpegPath: ffmpeg, FFprobePath: ffprobe}}}},
		netease,
	}
	disableLocalLyricsImport(&cfg)
	startNCMServer(t, cfg, base)
	c := login(t, base, cfg.AdminToken)
	account := authenticatePlaylistFixture(t, c)
	// 旧来源先于本地文件建档。同一次歌单同步也必须保留它原有的曲目身份。
	old := albumRequest[library.Track](t, c, "POST", remote+"/tracks/43/resolve", struct{}{}, "", 200, "SourceTrack")
	scanned := scanNCM(t, c)
	local := scanned["one.mp3"]
	if local.Title == nil || *local.Title != "New song" || local.DurationMs == nil || local.Album == nil || len(local.Artists) == 0 {
		t.Fatalf("local file was not scanned with its music tags: %+v", local)
	}
	revision := albumRequest[struct{ Revision int64 }](t, c, "GET", "/matching/revision", nil, "", 200, "MatchingRevision")
	group := albumRequest[matching.Mutation](t, c, "POST", "/matching/links", matching.LinkRequest{
		ExpectedRevision: revision.Revision, SourceTrackIDs: []string{local.ID, scanned["two.mp3"].ID},
	}, "existing-local-recording", 200, "MatchingMutation")
	if group.RecordingID == nil {
		t.Fatal("missing local recording")
	}
	code, raw, _ := c.request(t, "PUT", "/api/v1"+remote+"/playlists/91/link", map[string]string{"accountId": account, "playlistId": "favorites"}, "")
	var link playlists.PlaylistLink
	if code != 200 || json.Unmarshal(raw, &link) != nil || link.JobID == "" {
		t.Fatalf("link favorites: %d %s", code, raw)
	}
	for deadline := time.Now().Add(30 * time.Second); ; {
		job := albumRequest[playlists.SyncJob](t, c, "GET", "/remote-playlist-sync-jobs/"+link.JobID, nil, "", 200, "RemotePlaylistSyncJob")
		if job.State == "succeeded" {
			break
		}
		if job.State == "failed" || job.State == "cancelled" || time.Now().After(deadline) {
			t.Fatalf("import did not finish: %+v", job)
		}
		time.Sleep(100 * time.Millisecond)
	}
	favorites := albumRequest[personal.Page](t, c, "GET", "/me/favorites", nil, "", 200, "PersonalCollectionPage")
	if len(favorites.Items) != 2 {
		t.Fatalf("unexpected favorites count: %d", len(favorites.Items))
	}
	got := map[string]music.Track{}
	for _, entry := range favorites.Items {
		got[entry.MusicTrack.ID] = entry.MusicTrack
	}
	if merged := got[*group.RecordingID]; len(merged.Sources) != 3 || merged.SelectedSource == nil || merged.SelectedSource.ProviderID != localID {
		t.Fatalf("favorite did not retain the local recording and its sources: %+v", merged)
	}
	if len(got[old.ID].Sources) != 1 {
		t.Fatal("sync regrouped an online source that already existed")
	}
}
