package e2e_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writebackhost"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func TestManagedWritebackDisabledStartAndChangedRootFence(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if runtime.GOOS != "windows" || ffmpeg == "" || ffprobe == "" {
		t.Skip("requires Windows NTFS and MUSICFS_FFMPEG/MUSICFS_FFPROBE")
	}
	provider, audio := build(t, "musicfs-plugin-local"), build(t, "musicfs-plugin-audio-ffmpeg")
	original, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "lossless.flac"))
	if err != nil {
		t.Fatal(err)
	}
	music, replacement := t.TempDir(), t.TempDir()
	for name, root := range map[string]string{"source.flac": music, "replacement.flac": replacement} {
		if err = os.WriteFile(filepath.Join(root, name), original, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{
		{PluginID: "musicfs.plugin.source.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Enabled: false, Permissions: &config.Permissions{ReadDirectories: []string{music, replacement}}, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Music", Path: music}}}}}},
		{PluginID: "musicfs.plugin.decoder.ffmpeg", Executable: audio, Enabled: true, Instances: []config.Instance{{InstanceID: "16b003e5-1bab-52bb-be10-0e83ec1c40ea", Enabled: false, Config: config.InstanceConfig{FFmpegPath: ffmpeg, FFprobePath: ffprobe}}}},
	}
	cfg.WriteBack = config.WriteBackConfig{Enabled: true, MaxFileBytes: 8 << 30, Roots: []config.WriteBackRoot{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", RootID: "local", Fields: []string{"lyrics"}}}}
	path := writeYAMLConfiguration(t, cfg)
	stop := startAudioYAMLServer(t, path, cfg, base)
	defer func() { stop() }()
	c := login(t, base, cfg.AdminToken)
	if cap := writeBackCapability(t, c); !cap.Enabled || cap.Available {
		t.Fatalf("disabled source advertised available writeback: %+v", cap)
	}
	for _, id := range []string{"16b003e5-1bab-52bb-be10-0e83ec1c40ea", "b1548e96-3081-5c9c-8376-bf290e369d51"} {
		op := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/"+id+"/state", map[string]any{"enabled": true, "mode": "drain"})
		waitLibraryOperation(t, c, op.ID)
	}
	if cap := writeBackCapability(t, c); !cap.Enabled || !cap.Available {
		t.Fatalf("enabling original roots did not restore writeback: %+v", cap)
	}
	track := scanProvider(t, c, "b1548e96-3081-5c9c-8376-bf290e369d51", "managed-writeback-original-scan")["source.flac"]
	request := selectManagedWritebackLyrics(t, c, track, "managed-writeback-first")
	jobID := submitWriteBack(t, c, request, "managed-writeback-enabled")
	var job writebackhost.Job
	for deadline := time.Now().Add(15 * time.Second); ; {
		job = getWriteBack(t, c, jobID)
		if job.Status != "queued" && job.Status != "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("managed writeback timed out: %+v", job)
		}
		time.Sleep(30 * time.Millisecond)
	}
	if job.Status != "succeeded" || job.Result == nil || !job.Result.Changed {
		t.Fatalf("writeback after initial enable failed: %+v", job)
	}
	written, err := os.ReadFile(filepath.Join(music, "source.flac"))
	if err != nil || !bytes.Contains(written, []byte("Managed writeback lyrics")) {
		t.Fatal("writeback did not update the authorized original", err)
	}
	track = scanProvider(t, c, "b1548e96-3081-5c9c-8376-bf290e369d51", "managed-writeback-after-write")["source.flac"]
	request.AssetID, request.AssetVersion = track.AssetID, track.Asset.Version
	paused := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/16b003e5-1bab-52bb-be10-0e83ec1c40ea/state", map[string]any{"enabled": false, "mode": "drain"})
	waitLibraryOperation(t, c, paused.ID)
	if cap := writeBackCapability(t, c); !paused.RestartRequired || !cap.Available {
		t.Fatal("pending probe disable removed current writeback availability")
	}
	stop()
	stop = startAudioYAMLServer(t, path, cfg, base)
	c = login(t, base, cfg.AdminToken)
	if cap := writeBackCapability(t, c); cap.Available {
		t.Fatal("disabled probe advertised writeback availability")
	}
	code, raw, _ := c.request(t, "POST", "/api/v1/writeback-jobs", request, "managed-writeback-probe-disabled")
	if code != 415 || !bytes.Contains(raw, []byte("UNSUPPORTED")) {
		t.Fatalf("writeback started without validation tools: %d %s", code, raw)
	}
	resumed := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/16b003e5-1bab-52bb-be10-0e83ec1c40ea/state", map[string]any{"enabled": true, "mode": "drain"})
	waitLibraryOperation(t, c, resumed.ID)
	if cap := writeBackCapability(t, c); !cap.Available {
		t.Fatal("restarted probe did not restore writeback availability")
	}

	change := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/b1548e96-3081-5c9c-8376-bf290e369d51/configuration", map[string]any{"musicRoots": []config.MusicRoot{{ID: "local", Name: "Music", Path: music}, {ID: "replacement", Name: "Replacement music", Path: replacement}}})
	waitLibraryOperation(t, c, change.ID)
	code, raw, _ = c.request(t, "GET", "/api/v1/provider-instances", nil, "")
	var providers struct {
		Items []library.Provider `json:"items"`
	}
	if code != 200 || json.Unmarshal(raw, &providers) != nil {
		t.Fatalf("provider capabilities: %d %s", code, raw)
	}
	for _, provider := range providers.Items {
		for _, root := range provider.Roots {
			if root.ID == "replacement" && (root.WriteBack == nil || root.WriteBack.Enabled || root.WriteBack.Available) {
				t.Fatalf("replacement root inherited original writeback grant: %+v", root.WriteBack)
			}
		}
	}
	track = scanProvider(t, c, "b1548e96-3081-5c9c-8376-bf290e369d51", "managed-writeback-replacement-scan")["replacement.flac"]
	request = selectManagedWritebackLyrics(t, c, track, "managed-writeback-replacement")
	code, raw, _ = c.request(t, "POST", "/api/v1/writeback-jobs", request, "managed-writeback-root-denied")
	if code != 403 || !bytes.Contains(raw, []byte("ACCESS_DENIED")) {
		t.Fatalf("changed root was accepted using a previous path grant: %d %s", code, raw)
	}
	for filename, expected := range map[string][]byte{filepath.Join(music, "source.flac"): written, filepath.Join(replacement, "replacement.flac"): original} {
		actual, err := os.ReadFile(filename)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatal("rejected write changed source", err)
		}
	}
}

func selectManagedWritebackLyrics(t *testing.T, c *client, track library.Track, key string) writebackhost.Request {
	t.Helper()
	if track.ID == "" || track.Asset == nil {
		t.Fatal("writeback source missing")
	}
	target := mediastore.ContentTarget{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version}
	object := uploadActualMedia(t, c, target, "lyrics", "und", "text/plain", []byte("Managed writeback lyrics\n"), key+"-upload")
	input := map[string]any{"objectId": object.ID, "assetId": target.AssetID, "assetVersion": target.AssetVersion, "expectedRevision": 0}
	code, raw, _ := c.request(t, "PUT", "/api/v1/source-tracks/"+track.ID+"/media-selection", input, key+"-select")
	var selection struct {
		Revision int64 `json:"revision"`
	}
	if code != 200 || json.Unmarshal(raw, &selection) != nil || selection.Revision != 1 {
		t.Fatalf("writeback selection failed: %d %s", code, raw)
	}
	return writebackhost.Request{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version, ExpectedRevision: 1, Fields: []writebackhost.Field{{Name: "lyrics", Language: "und"}}}
}
