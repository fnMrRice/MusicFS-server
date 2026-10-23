package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

func TestYAMLChannelPluginDisabledByDefault(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for disabled channel plugin startup validation")
	}
	music := t.TempDir()
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(music, "source.wav"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, base := yamlServiceConfig(t)
	// 旧布局仍在配置中，但省略 enabled 必须真正禁用插件。
	cfg.AudioChannels.OutputLayout = "7.1"
	cfg.Plugins = []config.Plugin{
		{PluginID: "musicfs.plugin.source.localfiles", Execution: "builtin", Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Music", Path: music}}}}}},
		{PluginID: "musicfs.plugin.decoder.ffmpeg", Execution: "builtin", Enabled: true, Instances: []config.Instance{{InstanceID: "16b003e5-1bab-52bb-be10-0e83ec1c40ea", Enabled: true, Config: config.InstanceConfig{FFmpegPath: ffmpeg, FFprobePath: ffprobe}}}},
	}
	disableLocalLyricsImport(&cfg)
	startAudioYAMLServer(t, writeYAMLConfiguration(t, cfg), cfg, base)
	c := login(t, base, cfg.AdminToken)
	track := scanNCM(t, c)["source.wav"]
	code, raw, _ := c.request(t, "GET", "/api/v1/admin/plugins", nil, "")
	var plugins plugincontrol.Snapshot
	if code != 200 || json.Unmarshal(raw, &plugins) != nil {
		t.Fatalf("plugin list failed: %d %s", code, raw)
	}
	found := false
	for _, plugin := range plugins.Items {
		if plugin.PluginID == "musicfs.plugin.decoder.channelmix" {
			found = true
			if plugin.ConfiguredEnabled || plugin.Builtin == nil || plugin.Builtin.Configured {
				t.Fatal("disabled channel plugin was advertised as active", plugin)
			}
		}
	}
	if !found {
		t.Fatal("disabled channel plugin disappeared from management")
	}
	for _, execution := range []string{"server", "client"} {
		request := playback.CreateRequest{SourceTrackID: track.ID, Preferences: playback.Preferences{DSPExecution: execution},
			Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}, {MIME: "audio/mpeg", CanPlay: "probably"}}}}
		for _, processed := range []bool{false, true} {
			request.Preferences.ForceProcessing = processed
			key, mode := execution+"-channel-original", "proxy"
			if processed {
				key, mode = execution+"-channel-transcode", "processed"
			}
			code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", request, key)
			var description playback.Description
			if code != 201 || json.Unmarshal(raw, &description) != nil || description.DeliveryMode != mode || description.OutputProfile.Channels != 2 || len(description.EffectiveProcessing) != 0 {
				t.Fatalf("disabled plugin changed playback: status=%d body=%s", code, raw)
			}
			code, raw, _ = c.request(t, "GET", description.StreamURL, nil, "")
			if code != 200 || len(raw) < 1000 {
				t.Fatalf("disabled plugin blocked stream: status=%d bytes=%d", code, len(raw))
			}
		}
	}
}
