package e2e_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestManagedAudioDrainSeekRestartAndNCMForce(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for actual audio lifecycle validation")
	}
	provider := build(t, "musicfs-plugin-local")
	audio := build(t, "musicfs-plugin-audio-ffmpeg")
	decoder := build(t, "musicfs-plugin-ncm")
	flac, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "lossless.flac"))
	if err != nil {
		t.Fatal(err)
	}
	mp3, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "vbr-untagged.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := testkit.BuildNCM(mp3, map[string]any{"musicName": "Managed encrypted song", "format": "mp3"})
	if err != nil {
		t.Fatal(err)
	}
	music := t.TempDir()
	for name, payload := range map[string][]byte{"source.flac": flac, "source.ncm": encrypted} {
		if err = os.WriteFile(filepath.Join(music, name), payload, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{
		{PluginID: "musicfs.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "local", Role: "provider", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Music", Path: music}}}}}},
		{PluginID: "musicfs.audio.ffmpeg", Executable: audio, Enabled: true, Instances: []config.Instance{{InstanceID: "audio-default", Role: "audio", Enabled: true, Config: config.InstanceConfig{FFmpegPath: ffmpeg, FFprobePath: ffprobe}}}},
		{PluginID: "musicfs.ncm", Executable: decoder, Enabled: true, Instances: []config.Instance{{InstanceID: "ncm-default", Role: "audio", Enabled: true}}},
	}
	path := writeYAMLConfiguration(t, cfg)
	stop := startAudioYAMLServer(t, path, cfg, base)
	c := login(t, base, cfg.AdminToken)
	tracks := scanNCM(t, c)
	flacTrack, ncmTrack := tracks["source.flac"], tracks["source.ncm"]
	if flacTrack.ID == "" || ncmTrack.ID == "" {
		t.Fatal("source scan did not publish both fixtures")
	}

	first := createYAMLProcessedPlayback(t, c, flacTrack.ID, "managed-audio-initial")
	draining := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/audio-default/state", map[string]any{"enabled": false, "mode": "drain"})
	request := playback.CreateRequest{SourceTrackID: flacTrack.ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/mpeg", CanPlay: "probably"}}}}
	code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", request, "managed-audio-rejected")
	if code != 503 || !bytes.Contains(raw, []byte("PLUGIN_DISABLED")) {
		t.Fatalf("new playback entered draining audio plugin: %d %s", code, raw)
	}
	state := managedAudioInstance(t, c, "audio-default")
	if state.AcceptingNewWork || state.ActiveLeases < 1 || state.RuntimeState != "draining" {
		t.Fatalf("audio drain did not retain the old session: %+v", state)
	}
	full := fetchYAMLAudioPCM(t, c, ffmpeg, first.StreamURL)
	assertYAMLAudioDuration(t, full, 6000)
	assertYAMLAudioTone(t, full, 300, 1300, 220)
	assertYAMLAudioTone(t, full, 4300, 5300, 660)
	control := playback.ControlRequest{AttemptID: first.AttemptID, PositionMs: 4000}
	code, raw, _ = c.request(t, "POST", "/api/v1/playback/sessions/"+first.SessionID+"/seek", control, "managed-audio-drain-seek")
	var seek playback.Description
	if code != 200 || json.Unmarshal(raw, &seek) != nil || seek.AttemptID == first.AttemptID || seek.PositionOriginMs != 4000 {
		t.Fatalf("old session could not seek during drain: %d %s", code, raw)
	}
	segment := fetchYAMLAudioPCM(t, c, ffmpeg, seek.StreamURL)
	assertYAMLAudioDuration(t, segment, 2000)
	assertYAMLAudioTone(t, segment, 250, 1250, 660)
	stopManagedAudioPlayback(t, c, first.SessionID)
	waitLibraryOperation(t, c, draining.ID)
	state = managedAudioInstance(t, c, "audio-default")
	if state.EffectiveEnabled || state.ActiveLeases != 0 || state.RuntimeState != "disabled" {
		t.Fatalf("audio plugin did not release all work before stopping: %+v", state)
	}
	restarting := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/audio-default/state", map[string]any{"enabled": true, "mode": "drain"})
	waitLibraryOperation(t, c, restarting.ID)
	restarted := createYAMLProcessedPlayback(t, c, flacTrack.ID, "managed-audio-restarted")
	if restarted.AssetID != first.AssetID || restarted.SessionID == first.SessionID {
		t.Fatal("audio restart did not preserve source identity and replace the session")
	}
	full = fetchYAMLAudioPCM(t, c, ffmpeg, restarted.StreamURL)
	assertYAMLAudioDuration(t, full, 6000)
	assertYAMLAudioTone(t, full, 2300, 3300, 440)
	stopManagedAudioPlayback(t, c, restarted.SessionID)

	request.SourceTrackID = ncmTrack.ID
	code, raw, _ = c.request(t, "POST", "/api/v1/playback/sessions", request, "managed-ncm-initial")
	var decrypted playback.Description
	if code != 201 || json.Unmarshal(raw, &decrypted) != nil || decrypted.DeliveryMode != "proxy" || decrypted.SeekMode != "byteRange" {
		t.Fatalf("NCM fixture did not produce a decrypted proxy session: %d %s", code, raw)
	}
	verifyRanges(t, c, decrypted, mp3)
	state = managedAudioInstance(t, c, "ncm-default")
	if state.ActiveLeases < 1 {
		t.Fatalf("active NCM session has no lifecycle lease: %+v", state)
	}
	forced := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/ncm-default/state", map[string]any{"enabled": false, "mode": "force"})
	waitLibraryOperation(t, c, forced.ID)
	state = managedAudioInstance(t, c, "ncm-default")
	if state.EffectiveEnabled || state.ActiveLeases != 0 || state.RuntimeState != "disabled" {
		t.Fatalf("NCM force stop did not release the active session: %+v", state)
	}
	if code, raw, _ = c.request(t, "GET", decrypted.StreamURL, nil, ""); code != 404 {
		t.Fatalf("force-stopped NCM stream is still reachable: %d %s", code, raw)
	}
	code, raw, _ = c.request(t, "POST", "/api/v1/playback/sessions", request, "managed-ncm-disabled")
	if code != 415 && (code != 503 || !bytes.Contains(raw, []byte("PLUGIN_DISABLED"))) {
		t.Fatalf("new NCM session accepted without decoder: %d %s", code, raw)
	}
	restarting = libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/ncm-default/state", map[string]any{"enabled": true, "mode": "drain"})
	waitLibraryOperation(t, c, restarting.ID)
	code, raw, _ = c.request(t, "POST", "/api/v1/playback/sessions", request, "managed-ncm-restarted")
	var recovered playback.Description
	if code != 201 || json.Unmarshal(raw, &recovered) != nil || recovered.SessionID == decrypted.SessionID || recovered.AssetID != decrypted.AssetID {
		t.Fatalf("NCM playback did not recover after restart: %d %s", code, raw)
	}
	verifyRanges(t, c, recovered, mp3)
	stopManagedAudioPlayback(t, c, recovered.SessionID)
	for name, expected := range map[string][]byte{"source.flac": flac, "source.ncm": encrypted} {
		actual, err := os.ReadFile(filepath.Join(music, name))
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("plugin lifecycle changed source %s: %v", name, err)
		}
	}
	stop()
}

func managedAudioInstance(t *testing.T, c *client, id string) plugincontrol.Instance {
	t.Helper()
	code, raw, _ := c.request(t, "GET", "/api/v1/admin/plugins", nil, "")
	var snapshot plugincontrol.Snapshot
	if code != 200 || json.Unmarshal(raw, &snapshot) != nil {
		t.Fatalf("plugin manager read failed: %d %s", code, raw)
	}
	for _, plugin := range snapshot.Items {
		for _, instance := range plugin.Instances {
			if instance.InstanceID == id {
				return instance
			}
		}
	}
	t.Fatalf("configured plugin instance %s was not returned", id)
	return plugincontrol.Instance{}
}

func stopManagedAudioPlayback(t *testing.T, c *client, id string) {
	t.Helper()
	if code, raw, _ := c.request(t, "DELETE", "/api/v1/playback/sessions/"+id, nil, ""); code != 204 {
		t.Fatalf("managed session cleanup failed: %d %s", code, raw)
	}
}
