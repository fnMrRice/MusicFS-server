package e2e_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

func TestYAMLSurroundDSPPlaybackAndRestart(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for YAML surround validation")
	}
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "lossless.flac"))
	if err != nil {
		t.Fatal(err)
	}
	music := t.TempDir()
	source := filepath.Join(music, "surround.flac")
	if err := os.WriteFile(source, payload, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, base := yamlServiceConfig(t)
	cfg.AudioChannels = config.AudioChannelsConfig{OutputLayout: "5.1"}
	cfg.AudioDSP = config.AudioDSPConfig{Chain: []audio.NodeSpec{
		{Name: "equalizer", Version: 1, Params: map[string]float64{"frequencyHz": 440, "gainDB": -6, "q": 1}},
		{Name: "compressor", Version: 1, Params: map[string]float64{"thresholdDB": -24, "ratio": 4}},
		{Name: "reverb", Version: 1, Params: map[string]float64{"wet": 0.2}},
	}}
	cfg.Plugins = []config.Plugin{
		{PluginID: "musicfs.plugin.source.localfiles", Execution: "builtin", Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Music", Path: music}}}}}},
		{PluginID: "musicfs.plugin.decoder.ffmpeg", Execution: "builtin", Enabled: true, Instances: []config.Instance{{InstanceID: "16b003e5-1bab-52bb-be10-0e83ec1c40ea", Enabled: true, Config: config.InstanceConfig{FFmpegPath: ffmpeg, FFprobePath: ffprobe}}}},
	}
	path := writeYAMLConfiguration(t, cfg)
	originalYAML, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stop := startAudioYAMLServer(t, path, cfg, base)
	c := login(t, base, cfg.AdminToken)
	track := scanNCM(t, c)["surround.flac"]
	if track.ID == "" || track.DurationMs == nil || *track.DurationMs != 6000 {
		t.Fatalf("missing scanned audio source: %+v", track)
	}
	first := createYAMLSurroundPlayback(t, c, track.ID, "yaml-surround-initial")
	before := fetchYAMLSurroundPCM(t, c, ffmpeg, ffprobe, first.StreamURL)
	stop()
	startAudioYAMLServer(t, path, cfg, base)
	c = login(t, base, cfg.AdminToken)
	code, raw, _ := c.request(t, "GET", "/api/v1/source-tracks", nil, "")
	var page library.Page
	if code != 200 || json.Unmarshal(raw, &page) != nil || len(page.Items) != 1 || page.Items[0].ID != track.ID || page.Items[0].AssetID != track.AssetID {
		t.Fatalf("restart lost the surround source identity: %d %s", code, raw)
	}
	if code, _, _ := c.request(t, "GET", first.StreamURL, nil, ""); code != 404 {
		t.Fatalf("old surround session survived restart: %d", code)
	}
	restarted := createYAMLSurroundPlayback(t, c, track.ID, "yaml-surround-restarted")
	if restarted.SessionID == first.SessionID || restarted.AssetID != first.AssetID {
		t.Fatal("restart failed to create a new session for the same source")
	}
	after := fetchYAMLSurroundPCM(t, c, ffmpeg, ffprobe, restarted.StreamURL)
	if !bytes.Equal(before, after) {
		t.Fatal("restart changed decoded surround audio or DSP configuration")
	}
	currentYAML, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(originalYAML, currentYAML) {
		t.Fatal("restart modified the YAML surround configuration")
	}
	currentSource, err := os.ReadFile(source)
	if err != nil || !bytes.Equal(payload, currentSource) {
		t.Fatal("surround processing modified the source")
	}
	t.Log("YAML startup and restart retained builtin EQ, compressor, reverb and six-channel Opus/WebM output")
}

func createYAMLSurroundPlayback(t *testing.T, c *client, trackID, key string) playback.Description {
	t.Helper()
	request := playback.CreateRequest{SourceTrackID: trackID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/webm; codecs=opus", CanPlay: "probably"}}}}
	code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", request, key)
	var description playback.Description
	if code != 201 || json.Unmarshal(raw, &description) != nil {
		t.Fatalf("YAML surround negotiation failed: %d %s", code, raw)
	}
	profile := description.OutputProfile
	if description.DeliveryMode != "processed" || profile.ID != "opus-5.1" || profile.MIME != "audio/webm" || profile.Channels != 6 || profile.Layout != "5.1" || profile.Codec == nil || *profile.Codec != "opus" || profile.Container == nil || *profile.Container != "webm" {
		t.Fatalf("YAML output layout did not reach playback: %+v", description)
	}
	want := []string{"channelMix", "gain", "equalizer", "compressor", "reverb"}
	if len(description.EffectiveProcessing) != len(want) {
		t.Fatalf("YAML DSP chain did not reach playback: %+v", description.EffectiveProcessing)
	}
	for i, name := range want {
		if description.EffectiveProcessing[i].Type != name || description.EffectiveProcessing[i].Execution != "server" {
			t.Fatalf("unexpected configured DSP step %d: %+v", i, description.EffectiveProcessing[i])
		}
	}
	if description.EffectiveProcessing[2].Parameters["gainDB"] != -6 || description.EffectiveProcessing[3].Parameters["thresholdDB"] != -24 || description.EffectiveProcessing[4].Parameters["wet"] != 0.2 {
		t.Fatalf("YAML DSP parameters were lost: %+v", description.EffectiveProcessing)
	}
	return description
}

func fetchYAMLSurroundPCM(t *testing.T, c *client, ffmpeg, ffprobe, streamURL string) []byte {
	t.Helper()
	code, encoded, headers := c.request(t, "GET", streamURL, nil, "")
	if code != 200 || headers.Get("Content-Type") != "audio/webm" || len(encoded) < 100 {
		t.Fatalf("surround delivery failed: status=%d bytes=%d content-type=%q", code, len(encoded), headers.Get("Content-Type"))
	}
	probe := runYAMLSurroundTool(t, ffprobe, encoded, "-v", "error", "-show_entries", "stream=codec_name,channels,channel_layout", "-of", "json", "pipe:0")
	var metadata struct {
		Streams []struct {
			Codec    string `json:"codec_name"`
			Channels int    `json:"channels"`
			Layout   string `json:"channel_layout"`
		} `json:"streams"`
	}
	if json.Unmarshal(probe, &metadata) != nil || len(metadata.Streams) != 1 || metadata.Streams[0].Codec != "opus" || metadata.Streams[0].Channels != 6 || metadata.Streams[0].Layout != "5.1" {
		t.Fatalf("actual surround stream does not match its API description: %s", probe)
	}
	pcm := runYAMLSurroundTool(t, ffmpeg, encoded, "-v", "error", "-i", "pipe:0", "-f", "f32le", "-ar", "48000", "pipe:1")
	if len(pcm)%24 != 0 || math.Abs(float64(len(pcm))/24/48-6000) > 100 {
		t.Fatalf("unexpected six-channel PCM length: %d", len(pcm))
	}
	var energy [6]float64
	frames := len(pcm) / 24
	for frame := range frames {
		for channel := range 6 {
			value := float64(math.Float32frombits(binary.LittleEndian.Uint32(pcm[frame*24+channel*4:])))
			if math.IsNaN(value) || math.IsInf(value, 0) {
				t.Fatal("surround stream contains non-finite PCM")
			}
			energy[channel] += value * value
		}
	}
	for channel, sum := range energy {
		rms := math.Sqrt(sum / float64(frames))
		if channel < 2 && rms < 0.005 || channel >= 2 && rms > 0.001 {
			t.Fatalf("stereo-to-surround routing changed speaker content: channel=%d RMS=%g", channel, rms)
		}
	}
	return pcm
}

func runYAMLSurroundTool(t *testing.T, executable string, input []byte, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, args...)
	command.Stdin = bytes.NewReader(input)
	var diagnostics bytes.Buffer
	command.Stderr = &diagnostics
	output, err := command.Output()
	if err != nil {
		t.Fatalf("surround audio tool failed: %v %s", err, diagnostics.String())
	}
	return output
}
