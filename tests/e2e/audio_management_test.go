package e2e_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestManagedAudioPendingDisableSeekAndServerRestart(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for actual audio lifecycle validation")
	}
	provider := build(t, "musicfs-plugin-local")
	audio := build(t, "musicfs-plugin-audio-ffmpeg")
	decoder := build(t, "musicfs-plugin-ncm")
	scraper := build(t, "musicfs-plugin-scraper-local")
	flac, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "lossless.flac"))
	if err != nil {
		t.Fatal(err)
	}
	mp3, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "vbr-untagged.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	lyrics := []byte("[00:01.00]Synthetic pending disable lyrics")
	tag := testkit.ScraperID3(4, testkit.ScraperFrame{ID: "USLT", Body: append([]byte{3, 'e', 'n', 'g', 0}, lyrics...)})
	mp3 = append(tag, mp3...)
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
		{PluginID: "musicfs.plugin.source.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Music", Path: music}}}}}},
		{PluginID: "musicfs.plugin.decoder.ffmpeg", Executable: audio, Enabled: true, Instances: []config.Instance{{InstanceID: "16b003e5-1bab-52bb-be10-0e83ec1c40ea", Enabled: true, Config: config.InstanceConfig{FFmpegPath: ffmpeg, FFprobePath: ffprobe}}}},
		{PluginID: "musicfs.plugin.decoder.ncm", Executable: decoder, Enabled: true, Instances: []config.Instance{{InstanceID: "96412f52-9ff4-5a82-9eaa-7644a5837cda", Enabled: true}}},
		{PluginID: "musicfs.scraper.local", Executable: scraper, Enabled: true, Instances: []config.Instance{{InstanceID: "d248bf34-1780-59d3-a557-9cf5d889e977", Enabled: true}}},
	}
	path := writeYAMLConfiguration(t, cfg)
	stop := startAudioYAMLServer(t, path, cfg, base)
	defer func() { stop() }()
	c := login(t, base, cfg.AdminToken)
	tracks := scanNCM(t, c)
	flacTrack, ncmTrack := tracks["source.flac"], tracks["source.ncm"]
	if flacTrack.ID == "" || ncmTrack.ID == "" {
		t.Fatal("source scan did not publish both fixtures")
	}

	first := createYAMLProcessedPlayback(t, c, flacTrack.ID, "managed-audio-initial")
	audioGeneration := managedAudioInstance(t, c, "16b003e5-1bab-52bb-be10-0e83ec1c40ea").Generation
	pending := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/16b003e5-1bab-52bb-be10-0e83ec1c40ea/state", map[string]any{"enabled": false, "mode": "drain"})
	waitLibraryOperation(t, c, pending.ID)
	request := playback.CreateRequest{SourceTrackID: flacTrack.ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/mpeg", CanPlay: "probably"}}}}
	code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", request, "managed-audio-pending")
	var admitted playback.Description
	if code != 201 || json.Unmarshal(raw, &admitted) != nil {
		t.Fatalf("pending disable rejected new audio playback: %d %s", code, raw)
	}
	state := managedAudioInstance(t, c, "16b003e5-1bab-52bb-be10-0e83ec1c40ea")
	if !state.AcceptingNewWork || !state.EffectiveEnabled || state.ActiveLeases < 2 || state.RuntimeState != "ready" || state.Generation != audioGeneration || state.Reason == nil || *state.Reason != "RESTART_REQUIRED" {
		t.Fatalf("pending disable changed the audio runtime: %+v", state)
	}
	stopManagedAudioPlayback(t, c, admitted.SessionID)
	full := fetchYAMLAudioPCM(t, c, ffmpeg, first.StreamURL)
	assertYAMLAudioDuration(t, full, 6000)
	assertYAMLAudioTone(t, full, 300, 1300, 220)
	assertYAMLAudioTone(t, full, 4300, 5300, 660)
	control := playback.ControlRequest{AttemptID: first.AttemptID, PositionMs: 4000}
	code, raw, _ = c.request(t, "POST", "/api/v1/playback/sessions/"+first.SessionID+"/seek", control, "managed-audio-pending-seek")
	var seek playback.Description
	if code != 200 || json.Unmarshal(raw, &seek) != nil || seek.AttemptID == first.AttemptID || seek.PositionOriginMs != 4000 {
		t.Fatalf("old session could not seek while disable was pending: %d %s", code, raw)
	}
	segment := fetchYAMLAudioPCM(t, c, ffmpeg, seek.StreamURL)
	assertYAMLAudioDuration(t, segment, 2000)
	assertYAMLAudioTone(t, segment, 250, 1250, 660)
	stopManagedAudioPlayback(t, c, first.SessionID)
	state = managedAudioInstance(t, c, "16b003e5-1bab-52bb-be10-0e83ec1c40ea")
	if !state.EffectiveEnabled || state.ActiveLeases != 0 || state.RuntimeState != "ready" || state.Generation != audioGeneration {
		t.Fatalf("audio plugin stopped when pending-disable work completed: %+v", state)
	}
	restarting := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/16b003e5-1bab-52bb-be10-0e83ec1c40ea/state", map[string]any{"enabled": true, "mode": "drain"})
	waitLibraryOperation(t, c, restarting.ID)
	if state = managedAudioInstance(t, c, "16b003e5-1bab-52bb-be10-0e83ec1c40ea"); state.Generation != audioGeneration || state.Reason != nil {
		t.Fatalf("undoing audio disable replaced the runtime: %+v", state)
	}
	restarted := createYAMLProcessedPlayback(t, c, flacTrack.ID, "managed-audio-restarted")
	if restarted.AssetID != first.AssetID || restarted.SessionID == first.SessionID {
		t.Fatal("undoing audio disable did not preserve source identity for a new session")
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
	state = managedAudioInstance(t, c, "96412f52-9ff4-5a82-9eaa-7644a5837cda")
	if state.ActiveLeases < 1 {
		t.Fatalf("active NCM session has no lifecycle lease: %+v", state)
	}
	ncmGeneration := state.Generation
	forced := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/96412f52-9ff4-5a82-9eaa-7644a5837cda/state", map[string]any{"enabled": false, "mode": "force"})
	waitLibraryOperation(t, c, forced.ID)
	state = managedAudioInstance(t, c, "96412f52-9ff4-5a82-9eaa-7644a5837cda")
	if !state.EffectiveEnabled || !state.AcceptingNewWork || state.ActiveLeases < 1 || state.RuntimeState != "ready" || state.Generation != ncmGeneration || state.Reason == nil || *state.Reason != "RESTART_REQUIRED" {
		t.Fatalf("legacy force mode changed the NCM runtime: %+v", state)
	}
	verifyRanges(t, c, decrypted, mp3)
	// Updating another audio plugin must keep the pending-disable NCM runtime
	// available to both playback and background metadata workers.
	configured := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/16b003e5-1bab-52bb-be10-0e83ec1c40ea/configuration", map[string]any{"name": "Updated audio"})
	waitLibraryOperation(t, c, configured.ID)
	if state = managedAudioInstance(t, c, "96412f52-9ff4-5a82-9eaa-7644a5837cda"); state.Generation != ncmGeneration || !state.AcceptingNewWork {
		t.Fatalf("FFmpeg configuration applied the pending NCM disable: %+v", state)
	}
	code, raw, _ = c.request(t, "POST", "/api/v1/playback/sessions", request, "managed-ncm-after-audio-settings")
	var recovered playback.Description
	if code != 201 || json.Unmarshal(raw, &recovered) != nil || recovered.SessionID == decrypted.SessionID || recovered.AssetID != decrypted.AssetID {
		t.Fatalf("FFmpeg configuration lost NCM playback before restart: %d %s", code, raw)
	}
	verifyRanges(t, c, recovered, mp3)
	job := metadataJob(t, c, metadataRequest([]string{ncmTrack.ID}, "lyrics"), "managed-ncm-pending-metadata")
	if job.State != "completed" || len(job.Targets) != 1 || len(job.Targets[0].Candidates) == 0 || job.Targets[0].Candidates[0].PreviewObjectID == nil {
		t.Fatalf("FFmpeg configuration lost background NCM decoding before restart: %+v", job)
	}
	stopManagedAudioPlayback(t, c, recovered.SessionID)
	stopManagedAudioPlayback(t, c, decrypted.SessionID)
	pending = libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/16b003e5-1bab-52bb-be10-0e83ec1c40ea/state", map[string]any{"enabled": false})
	waitLibraryOperation(t, c, pending.ID)
	stop()
	stop = startAudioYAMLServer(t, path, cfg, base)
	c = login(t, base, cfg.AdminToken)
	for _, id := range []string{"16b003e5-1bab-52bb-be10-0e83ec1c40ea", "96412f52-9ff4-5a82-9eaa-7644a5837cda"} {
		state = managedAudioInstance(t, c, id)
		if state.EffectiveEnabled || state.AcceptingNewWork || state.ActiveLeases != 0 || state.RuntimeState != "disabled" {
			t.Fatalf("server restart did not apply disable for %s: %+v", id, state)
		}
	}
	code, raw, _ = c.request(t, "POST", "/api/v1/playback/sessions", request, "managed-ncm-disabled-after-restart")
	if code != 415 && (code != 503 || !bytes.Contains(raw, []byte("PLUGIN_DISABLED"))) {
		t.Fatalf("new NCM session accepted after disabled server restart: %d %s", code, raw)
	}
	for _, id := range []string{"16b003e5-1bab-52bb-be10-0e83ec1c40ea", "96412f52-9ff4-5a82-9eaa-7644a5837cda"} {
		enabled := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/"+id+"/state", map[string]any{"enabled": true})
		waitLibraryOperation(t, c, enabled.ID)
	}
	code, raw, _ = c.request(t, "POST", "/api/v1/playback/sessions", request, "managed-ncm-enabled-after-restart")
	if code != 201 || json.Unmarshal(raw, &recovered) != nil || recovered.AssetID != decrypted.AssetID {
		t.Fatalf("dynamic enable did not restore NCM playback: %d %s", code, raw)
	}
	verifyRanges(t, c, recovered, mp3)
	stopManagedAudioPlayback(t, c, recovered.SessionID)
	restarted = createYAMLProcessedPlayback(t, c, flacTrack.ID, "managed-audio-enabled-after-restart")
	full = fetchYAMLAudioPCM(t, c, ffmpeg, restarted.StreamURL)
	assertYAMLAudioDuration(t, full, 6000)
	assertYAMLAudioTone(t, full, 2300, 3300, 440)
	stopManagedAudioPlayback(t, c, restarted.SessionID)
	for name, expected := range map[string][]byte{"source.flac": flac, "source.ncm": encrypted} {
		actual, err := os.ReadFile(filepath.Join(music, name))
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatalf("plugin lifecycle changed source %s: %v", name, err)
		}
	}
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
