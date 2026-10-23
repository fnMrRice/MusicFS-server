package e2e_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/app/bootstrap"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback"
)

func TestYAMLAudioPCMSeekAndRestart(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for actual YAML audio validation")
	}
	provider, audio := build(t, "musicfs-plugin-local"), build(t, "musicfs-plugin-audio-ffmpeg")
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "lossless.flac"))
	if err != nil {
		t.Fatal(err)
	}
	music := t.TempDir()
	source := filepath.Join(music, "source.flac")
	if err := os.WriteFile(source, payload, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{
		{PluginID: "musicfs.plugin.source.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Music", Path: music}}}}}},
		{PluginID: "musicfs.plugin.decoder.ffmpeg", Executable: audio, Enabled: true, Instances: []config.Instance{{InstanceID: "16b003e5-1bab-52bb-be10-0e83ec1c40ea", Enabled: true, Config: config.InstanceConfig{FFmpegPath: ffmpeg, FFprobePath: ffprobe}}}},
	}
	disableLocalLyricsImport(&cfg)
	path := writeYAMLConfiguration(t, cfg)
	originalYAML, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	stop := startAudioYAMLServer(t, path, cfg, base)
	c := login(t, base, cfg.AdminToken)
	track, ok := scanNCM(t, c)["source.flac"]
	if !ok || track.DurationMs == nil || *track.DurationMs != 6000 || track.Codec == nil || *track.Codec != "flac" {
		t.Fatalf("unexpected probed FLAC metadata: %+v", track)
	}
	first := createYAMLProcessedPlayback(t, c, track.ID, "yaml-audio-initial-001")
	full := fetchYAMLAudioPCM(t, c, ffmpeg, first.StreamURL)
	assertYAMLAudioDuration(t, full, 6000)
	assertYAMLAudioTone(t, full, 300, 1300, 220)
	assertYAMLAudioTone(t, full, 2300, 3300, 440)
	assertYAMLAudioTone(t, full, 4300, 5300, 660)

	control := playback.ControlRequest{AttemptID: first.AttemptID, PositionMs: 4000}
	code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions/"+first.SessionID+"/seek", control, "yaml-audio-seek-001")
	var seek playback.Description
	if code != 200 || json.Unmarshal(raw, &seek) != nil || seek.AttemptID == first.AttemptID || seek.RepresentationID == first.RepresentationID || seek.PositionOriginMs != 4000 || seek.SessionID != first.SessionID || seek.AssetID != first.AssetID {
		t.Fatalf("HTTP seek did not create the expected representation: %d %s", code, raw)
	}
	if code, _, _ := c.request(t, "GET", first.StreamURL, nil, ""); code != 409 {
		t.Fatalf("old attempt remained readable after seek: %d", code)
	}
	segment := fetchYAMLAudioPCM(t, c, ffmpeg, seek.StreamURL)
	assertYAMLAudioDuration(t, segment, 2000)
	assertYAMLAudioTone(t, segment, 250, 1250, 660)
	onset := -1
	for i, sample := range segment {
		if math.Abs(float64(sample)) > .005 {
			onset = i
			break
		}
	}
	if onset < 0 || onset > 4800 {
		t.Fatalf("seek marker onset exceeded 100ms tolerance: %d samples", onset)
	}
	t.Logf("YAML processed stream: full=%.3fms, seek=%.3fms, 660Hz onset=%.3fms", float64(len(full))/48, float64(len(segment))/48, float64(onset)/48)

	stop()
	startAudioYAMLServer(t, path, cfg, base)
	c = login(t, base, cfg.AdminToken)
	code, raw, _ = c.request(t, "GET", "/api/v1/source-tracks", nil, "")
	var page library.Page
	if code != 200 || json.Unmarshal(raw, &page) != nil || len(page.Items) != 1 || page.Items[0].ID != track.ID || page.Items[0].AssetID != track.AssetID {
		t.Fatalf("YAML restart changed persisted source identity: %d %s", code, raw)
	}
	if code, _, _ := c.request(t, "GET", seek.StreamURL, nil, ""); code != 404 {
		t.Fatalf("old playback survived server restart: %d", code)
	}
	restarted := createYAMLProcessedPlayback(t, c, track.ID, "yaml-audio-restarted-001")
	if restarted.AssetID != first.AssetID || restarted.SessionID == first.SessionID {
		t.Fatal("restarted playback did not retain source identity with a new session")
	}
	after := fetchYAMLAudioPCM(t, c, ffmpeg, restarted.StreamURL)
	assertYAMLAudioDuration(t, after, 6000)
	assertYAMLAudioTone(t, after, 300, 1300, 220)
	assertYAMLAudioTone(t, after, 2300, 3300, 440)
	assertYAMLAudioTone(t, after, 4300, 5300, 660)
	currentYAML, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(originalYAML, currentYAML) {
		t.Fatal("server restart changed the YAML configuration")
	}
	currentSource, err := os.ReadFile(source)
	if err != nil || !bytes.Equal(payload, currentSource) {
		t.Fatal("audio processing changed the source file")
	}
	t.Log("Same YAML restart retained the source and produced all three decoded PCM markers")
}

func startAudioYAMLServer(t *testing.T, path string, cfg config.Config, base string) func() {
	t.Helper()
	loaded, err := config.Load(path, map[string]string{"ADMIN_TOKEN": cfg.AdminToken, "LISTEN": cfg.Listen, "DATA_ROOT": cfg.DataRoot, "MEDIA_ROOT": cfg.MediaRoot, "CACHE_ROOT": cfg.CacheRoot, "WEB_ROOT": cfg.WebRoot})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var serveErr error
	go func() {
		serveErr = bootstrap.Serve(ctx, loaded, slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()
	stop := sync.OnceFunc(func() {
		cancel()
		select {
		case <-done:
			if serveErr != nil {
				t.Errorf("YAML audio server stopped with error: %v", serveErr)
			}
		case <-time.After(8 * time.Second):
			t.Error("YAML audio server shutdown timeout")
		}
	})
	t.Cleanup(stop)
	probe := &http.Client{Timeout: time.Second}
	for deadline := time.Now().Add(15 * time.Second); ; {
		response, err := probe.Get(base + "/api/v1/health/ready")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				return stop
			}
		}
		select {
		case <-done:
			t.Fatalf("YAML audio server startup failed: %v", serveErr)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("YAML audio server startup timeout")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func createYAMLProcessedPlayback(t *testing.T, c *client, trackID, key string) playback.Description {
	t.Helper()
	request := playback.CreateRequest{SourceTrackID: trackID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/mpeg", CanPlay: "probably"}}}}
	code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", request, key)
	var description playback.Description
	if code != 201 || json.Unmarshal(raw, &description) != nil || description.DeliveryMode != "processed" || description.OutputProfile.MIME != "audio/mpeg" || description.SeekMode != "timeRestart" || description.SeekableRange == nil || description.SeekableRange.EndMs != 6000 {
		t.Fatalf("YAML audio negotiation failed: %d %s", code, raw)
	}
	return description
}

func fetchYAMLAudioPCM(t *testing.T, c *client, ffmpeg, streamURL string) []float32 {
	t.Helper()
	code, raw, headers := c.request(t, "GET", streamURL, nil, "")
	if code != 200 || headers.Get("Content-Type") != "audio/mpeg" || len(raw) < 100 {
		t.Fatalf("processed MP3 delivery failed: %d (%d bytes)", code, len(raw))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-i", "pipe:0", "-f", "f32le", "-ac", "1", "-ar", "48000", "pipe:1")
	cmd.Stdin = bytes.NewReader(raw)
	decoded, err := cmd.Output()
	if err != nil || len(decoded) == 0 || len(decoded)%4 != 0 {
		t.Fatalf("processed MP3 could not be decoded to PCM: %v (%d bytes)", err, len(decoded))
	}
	pcm := make([]float32, len(decoded)/4)
	for i := range pcm {
		pcm[i] = math.Float32frombits(binary.LittleEndian.Uint32(decoded[i*4:]))
	}
	return pcm
}

func assertYAMLAudioDuration(t *testing.T, pcm []float32, expectedMs float64) {
	t.Helper()
	if actual := float64(len(pcm)) / 48; math.Abs(actual-expectedMs) > 100 {
		t.Fatalf("decoded audio duration outside 100ms tolerance: got %.3fms want %.0fms", actual, expectedMs)
	}
}

func assertYAMLAudioTone(t *testing.T, pcm []float32, startMs, endMs int, expectedHz float64) {
	t.Helper()
	start, end := startMs*48, endMs*48
	if start < 0 || end > len(pcm) || end <= start {
		t.Fatal("PCM marker window is unavailable")
	}
	window := pcm[start:end]
	energy, crossings := 0.0, 0
	for i, sample := range window {
		if math.IsNaN(float64(sample)) || math.IsInf(float64(sample), 0) {
			t.Fatal("decoded PCM contains non-finite samples")
		}
		energy += float64(sample) * float64(sample)
		if i > 0 && window[i-1] <= 0 && sample > 0 {
			crossings++
		}
	}
	rms := math.Sqrt(energy / float64(len(window)))
	frequency := float64(crossings) * 48000 / float64(len(window))
	if rms < .01 || math.Abs(frequency-expectedHz) > 3 {
		t.Fatalf("wrong PCM marker at %d-%dms: %.3fHz RMS=%.5f, want %.0fHz", startMs, endMs, frequency, rms, expectedHz)
	}
	t.Logf("PCM marker at %d-%dms: %.3fHz, RMS=%.5f", startMs, endMs, frequency, rms)
}
