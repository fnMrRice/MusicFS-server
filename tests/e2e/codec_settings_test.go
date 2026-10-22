package e2e_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/plugins/codecsettings"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestCodecSettingsApplyToPlainAndNCMPlaybackAfterRestart(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for codec settings playback validation")
	}
	plain, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "vbr-untagged.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := testkit.BuildNCM(plain, map[string]any{"musicName": "设置解码测试", "format": "flac"})
	if err != nil {
		t.Fatal(err)
	}
	music := t.TempDir()
	sources := map[string][]byte{"plain.mp3": plain, "wrapped.ncm": encrypted}
	for name, data := range sources {
		if err := os.WriteFile(filepath.Join(music, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{
		{PluginID: "musicfs.plugin.source.localfiles", Execution: "builtin", Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Music", Path: music}}}}}},
		{PluginID: codecsettings.FFmpegID, Execution: "builtin", Enabled: true, Instances: []config.Instance{{InstanceID: "16b003e5-1bab-52bb-be10-0e83ec1c40ea", Enabled: true, Config: config.InstanceConfig{FFmpegPath: ffmpeg, FFprobePath: ffprobe}}}},
		{PluginID: codecsettings.NCMID, Execution: "builtin", Enabled: true, Instances: []config.Instance{{InstanceID: "96412f52-9ff4-5a82-9eaa-7644a5837cda", Enabled: true}}},
	}
	path := writeYAMLConfiguration(t, cfg)
	stop := startAudioYAMLServer(t, path, cfg, base)
	c := login(t, base, cfg.AdminToken)
	assertCodecManagement(t, c, false)
	tracks := scanNCM(t, c)
	if len(tracks) != len(sources) {
		t.Fatal("codec fixture sources were not scanned", tracks)
	}
	for _, name := range []string{"plain.mp3", "wrapped.ncm"} {
		assertCodecHTTPPlayback(t, c, ffmpeg, tracks[name], "native-"+name, "proxy", plain)
	}
	page := readCodecSettings(t, c)
	if page.Values["decodeOutput"] != "native" {
		t.Fatal("new installation changed default playback encoding", page.Values)
	}
	request := settings.Request{ExpectedRevision: &page.Revision, SchemaRevision: page.Schema.Revision, Values: settings.Values{"decodeOutput": audioffmpeg.DecodeSigned16}}
	code, raw, _ := c.request(t, "PUT", "/api/v1/plugin-settings/"+codecsettings.FFmpegID, request, "codec-save-signed-pcm")
	if code != 200 {
		t.Fatalf("codec settings save failed: %d %s", code, raw)
	}
	testkit.ValidateHTTP(t, "PluginSettingsPage", raw)
	for _, name := range []string{"plain.mp3", "wrapped.ncm"} {
		assertCodecHTTPPlayback(t, c, ffmpeg, tracks[name], "selected-"+name, "processed", nil)
	}
	assertCodecManagement(t, c, true)
	stop()
	startAudioYAMLServer(t, path, cfg, base)
	c = login(t, base, cfg.AdminToken)
	if page := readCodecSettings(t, c); page.Values["decodeOutput"] != audioffmpeg.DecodeSigned16 {
		t.Fatal("restart lost selected decoder output", page.Values)
	}
	for _, name := range []string{"plain.mp3", "wrapped.ncm"} {
		assertCodecHTTPPlayback(t, c, ffmpeg, tracks[name], "restarted-"+name, "processed", nil)
	}
	assertCodecManagement(t, c, true)
	for name, original := range sources {
		current, err := os.ReadFile(filepath.Join(music, name))
		if err != nil || !bytes.Equal(original, current) {
			t.Fatal("codec settings playback changed original source", name, err)
		}
	}
}

func readCodecSettings(t *testing.T, c *client) settings.Page {
	t.Helper()
	code, raw, _ := c.request(t, "GET", "/api/v1/plugin-settings/"+codecsettings.FFmpegID, nil, "")
	var page settings.Page
	if code != 200 || json.Unmarshal(raw, &page) != nil {
		t.Fatalf("codec settings unavailable: %d %s", code, raw)
	}
	testkit.ValidateHTTP(t, "PluginSettingsPage", raw)
	return page
}

func assertCodecHTTPPlayback(t *testing.T, c *client, ffmpeg string, track library.Track, key, mode string, original []byte) {
	t.Helper()
	request := playback.CreateRequest{SourceTrackID: track.ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/mpeg", CanPlay: "probably"}}}}
	code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", request, key)
	var description playback.Description
	if code != 201 || json.Unmarshal(raw, &description) != nil || description.DeliveryMode != mode || description.OutputProfile.MIME != "audio/mpeg" {
		t.Fatalf("codec playback negotiation failed: %d %s", code, raw)
	}
	testkit.ValidateHTTP(t, "Playback", raw)
	if mode == "proxy" {
		code, raw, _ = c.request(t, "GET", description.StreamURL, nil, "")
		if code != 200 || !bytes.Equal(raw, original) {
			t.Fatalf("native codec playback changed audio: %d bytes=%d", code, len(raw))
		}
	} else {
		pcm := fetchYAMLAudioPCM(t, c, ffmpeg, description.StreamURL)
		// MP3 夹具带两秒静音前缀，既有清单给出的完整时长为 8040 ms。
		assertYAMLAudioDuration(t, pcm, 8040)
		assertYAMLAudioTone(t, pcm, 4250, 5750, 440)
	}
	if code, raw, _ = c.request(t, "DELETE", "/api/v1/playback/sessions/"+description.SessionID, nil, ""); code != 204 {
		t.Fatalf("codec playback stop failed: %d %s", code, raw)
	}
	if code, _, _ = c.request(t, "GET", description.StreamURL, nil, ""); code != 404 {
		t.Fatal("stopped codec stream remains available", code)
	}
}

func assertCodecManagement(t *testing.T, c *client, waitIdle bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; {
		code, raw, _ := c.request(t, "GET", "/api/v1/admin/plugins", nil, "")
		var snapshot plugincontrol.Snapshot
		if code != 200 || json.Unmarshal(raw, &snapshot) != nil {
			t.Fatalf("codec plugin snapshot failed: %d %s", code, raw)
		}
		seen, active := 0, 0
		for _, item := range snapshot.Items {
			if item.PluginID != codecsettings.FFmpegID && item.PluginID != codecsettings.NCMID {
				continue
			}
			seen++
			if !item.SettingsAvailable || item.Plugin == nil || len(item.Plugin.Features) != 1 || item.Plugin.Features[0] != "audio-codec" {
				t.Fatal("codec settings or classification missing", item)
			}
			for _, instance := range item.Instances {
				active += instance.ActiveLeases
			}
		}
		if seen != 2 {
			t.Fatal("codec plugin declarations are missing", seen)
		}
		if !waitIdle || active == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("stopped playback retained codec runtime leases", active)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
