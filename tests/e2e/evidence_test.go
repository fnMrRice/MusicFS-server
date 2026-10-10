package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestRealVBRMetadataEvidenceDoesNotBlockOriginalPlayback(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for real metadata evidence")
	}
	provider, audio := build(t, "musicfs-plugin-local"), build(t, "musicfs-plugin-audio-ffmpeg")
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "vbr-untagged.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	music := t.TempDir()
	if err := os.WriteFile(filepath.Join(music, "VBR 中文.mp3"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{
		{PluginID: "musicfs.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "local", Role: "provider", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "music", Name: "Music", Path: music}}}}}},
		{PluginID: "musicfs.audio.ffmpeg", Executable: audio, Enabled: true, Instances: []config.Instance{{InstanceID: "audio", Role: "audio", Enabled: true, Config: config.InstanceConfig{FFmpegPath: ffmpeg, FFprobePath: ffprobe}}}},
	}
	startNCMServer(t, cfg, base)
	c := login(t, base, cfg.AdminToken)
	track := scanNCM(t, c)["VBR 中文.mp3"]
	if track.Asset == nil || track.Asset.BitrateBps == nil || *track.Asset.BitrateBps <= 0 || track.Asset.BitrateOrigin == nil || track.Asset.HashStatus != "unknown" || track.Asset.FileSHA512 != nil || track.Asset.HashComputedAt != nil {
		t.Fatalf("real metadata evidence incomplete or invented a hash: %+v", track.Asset)
	}
	if track.Asset.BitrateMode != "unknown" || len(track.Asset.Evidence) != 1 || track.Asset.Evidence[0].Algorithm != "ffprobe" || track.Asset.Evidence[0].AlgorithmVersion == "" || track.Asset.Evidence[0].ConfigRevision != "first-audio-stream-v1" {
		t.Fatalf("unverified mode or missing actual probe provenance: %+v", track.Asset)
	}
	raw, err := json.Marshal(track)
	if err != nil {
		t.Fatal(err)
	}
	testkit.ValidateHTTP(t, "SourceTrack", raw)
	request := playback.CreateRequest{SourceTrackID: track.ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/mpeg", CanPlay: "probably"}}}}
	code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", request, "unknown-hash-can-play")
	var description playback.Description
	if code != 201 || json.Unmarshal(raw, &description) != nil || description.DeliveryMode != "proxy" {
		t.Fatalf("unknown hash blocked original playback: %d %s", code, raw)
	}
	verifyRanges(t, c, description, payload)
	rescanned := scanProvider(t, c, "local", "same-vbr-rescan")["VBR 中文.mp3"]
	if rescanned.ID != track.ID || rescanned.AssetID != track.AssetID || len(rescanned.Asset.Evidence) != 1 || rescanned.Asset.Evidence[0].ID != track.Asset.Evidence[0].ID {
		t.Fatal("unchanged real probe rescan recreated asset/evidence")
	}
}
