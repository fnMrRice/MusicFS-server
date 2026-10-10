package e2e_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"go.yaml.in/yaml/v3"
)

func writeYAMLConfiguration(t *testing.T, cfg config.Config) string {
	t.Helper()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "server.yaml")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestYAMLCLIStartsWithDisabledPlugin(t *testing.T) {
	server, provider := build(t, "musicfs"), build(t, "musicfs-plugin-local")
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.localfiles", Executable: provider, Enabled: false, Instances: []config.Instance{{InstanceID: "local", Role: "provider", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "music", Name: "Music", Path: t.TempDir()}}}}}}}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, server, "serve", "--config", writeYAMLConfiguration(t, cfg))
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	for _, value := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(value), "MUSICFS_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "MUSICFS_ADMIN_TOKEN="+cfg.AdminToken)
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	exited := false
	t.Cleanup(func() {
		cancel()
		if !exited {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("CLI shutdown timeout")
			}
		}
	})
	for deadline := time.Now().Add(15 * time.Second); ; {
		response, err := http.Get(base + "/api/v1/health/ready")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				break
			}
		}
		select {
		case err := <-done:
			exited = true
			t.Fatalf("CLI YAML startup failed: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("CLI YAML startup timeout")
		}
		time.Sleep(20 * time.Millisecond)
	}
	c := login(t, base, cfg.AdminToken)
	code, raw, _ := c.request(t, "GET", "/api/v1/provider-instances", nil, "")
	var providers struct {
		Items []library.Provider `json:"items"`
	}
	if code != 200 || json.Unmarshal(raw, &providers) != nil || len(providers.Items) != 1 || providers.Items[0].Availability != "offline" {
		t.Fatalf("CLI disabled provider state: %d %s", code, raw)
	}
	if _, err := os.Stat(filepath.Join(cfg.DataRoot, "providers", "local")); !os.IsNotExist(err) {
		t.Fatalf("disabled CLI provider initialized runtime state: %v", err)
	}
}

func TestYAMLAudioPluginEnableBoundaries(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for YAML audio runtime validation")
	}
	provider, audio := build(t, "musicfs-plugin-local"), build(t, "musicfs-plugin-audio-ffmpeg")
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "lossless.flac"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name             string
		plugin, instance bool
	}{{"enabled", true, true}, {"plugin-disabled", false, true}, {"instance-disabled", true, false}} {
		t.Run(test.name, func(t *testing.T) {
			music := t.TempDir()
			if err := os.WriteFile(filepath.Join(music, "source.flac"), payload, 0600); err != nil {
				t.Fatal(err)
			}
			cfg, base := yamlServiceConfig(t)
			cfg.Plugins = []config.Plugin{
				{PluginID: "musicfs.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "local", Role: "provider", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Music", Path: music}}}}}},
				{PluginID: "musicfs.audio.ffmpeg", Executable: audio, Enabled: test.plugin, Instances: []config.Instance{{InstanceID: "audio-default", Role: "audio", Enabled: test.instance, Config: config.InstanceConfig{FFmpegPath: ffmpeg, FFprobePath: ffprobe}}}},
			}
			startNCMServer(t, cfg, base)
			c := login(t, base, cfg.AdminToken)
			tracks := scanNCM(t, c)
			track, ok := tracks["source.flac"]
			if !ok {
				t.Fatal("missing FLAC source")
			}
			enabled := test.plugin && test.instance
			if (track.DurationMs != nil) != enabled {
				t.Fatalf("ffprobe did not follow explicit enable state: %+v", track)
			}
			request := playback.CreateRequest{SourceTrackID: track.ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/mpeg", CanPlay: "probably"}}}}
			code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", request, "yaml-audio-play-001")
			if !enabled {
				if code != 415 {
					t.Fatalf("disabled audio plugin started: %d %s", code, raw)
				}
				return
			}
			var description playback.Description
			if code != 201 || json.Unmarshal(raw, &description) != nil || description.DeliveryMode != "processed" || description.OutputProfile.MIME != "audio/mpeg" {
				t.Fatalf("YAML audio processing negotiation failed: %d %s", code, raw)
			}
			if code, raw, header := c.request(t, "GET", description.StreamURL, nil, ""); code != 200 || len(raw) < 100 || header.Get("Content-Type") != "audio/mpeg" {
				t.Fatalf("YAML audio processing stream failed: %d (%d bytes)", code, len(raw))
			}
		})
	}
}

func yamlServiceConfig(t *testing.T) (config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	base := "http://" + address
	return config.Config{ConfigVersion: 1, Listen: address, AllowLoopbackHTTP: true, AllowedOrigins: []string{base}, AdminToken: domain.NewID(), DataRoot: filepath.Join(dir, "data"), MediaRoot: filepath.Join(dir, "media"), CacheRoot: filepath.Join(dir, "cache"), MaxSessions: 8, MaxProcessing: 2, MaxCacheBytes: 8 << 20, SessionTTLSeconds: 3600, Hashing: config.HashingConfig{MaxConcurrent: 1, BytesPerSecond: 4 << 20}}, base
}

func TestYAMLProviderInstancesAndMultipleRoots(t *testing.T) {
	provider := build(t, "musicfs-plugin-local")
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	roots := make([]config.MusicRoot, 0, 2)
	for _, id := range []string{"root-one", "root-two"} {
		path := t.TempDir()
		if err := os.WriteFile(filepath.Join(path, id+".wav"), payload, 0600); err != nil {
			t.Fatal(err)
		}
		roots = append(roots, config.MusicRoot{ID: id, Name: id, Path: path})
	}
	secondary := t.TempDir()
	if err := os.WriteFile(filepath.Join(secondary, "secondary.wav"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{
		{PluginID: "musicfs.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{
			{InstanceID: "local", Name: "Two roots", Role: "provider", Enabled: true, Config: config.InstanceConfig{MusicRoots: roots}},
			{InstanceID: "secondary", Name: "Separate instance", Role: "provider", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "root-one", Name: "Independent root ID", Path: secondary}}}},
			{InstanceID: "offline", Name: "Disabled instance", Role: "provider", Enabled: false, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "absent", Name: "Absent", Path: filepath.Join(t.TempDir(), "missing")}}}},
			{InstanceID: "failed", Name: "Unavailable enabled root", Role: "provider", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "absent", Name: "Absent", Path: filepath.Join(t.TempDir(), "missing")}}}},
		}},
		{PluginID: "musicfs.audio.ffmpeg", Executable: filepath.Join(t.TempDir(), "missing-audio"), Enabled: false, Instances: []config.Instance{{InstanceID: "audio-disabled", Role: "audio", Enabled: true, Config: config.InstanceConfig{FFmpegPath: filepath.Join(t.TempDir(), "missing-ffmpeg"), FFprobePath: filepath.Join(t.TempDir(), "missing-ffprobe")}}}},
		{PluginID: "musicfs.ncm", Executable: filepath.Join(t.TempDir(), "missing-ncm"), Enabled: true, Instances: []config.Instance{{InstanceID: "ncm-disabled", Role: "audio", Enabled: false}}},
	}
	startNCMServer(t, cfg, base)
	c := login(t, base, cfg.AdminToken)
	code, raw, _ := c.request(t, "GET", "/api/v1/provider-instances", nil, "")
	var providers struct {
		Items []library.Provider `json:"items"`
	}
	if code != 200 || json.Unmarshal(raw, &providers) != nil || len(providers.Items) != 4 {
		t.Fatalf("provider identity mismatch: %d %s", code, raw)
	}
	states := map[string]string{}
	for _, p := range providers.Items {
		states[p.ID] = p.Availability
	}
	if states["local"] != "available" || states["secondary"] != "available" || states["offline"] != "offline" || states["failed"] != "offline" {
		t.Fatalf("provider enable states ignored: %s", raw)
	}
	if code, _, _ := c.request(t, "POST", "/api/v1/provider-instances/offline/scan-jobs", providerScanRequest(t, c, "offline"), "offline-scan-001"); code != 503 {
		t.Fatalf("disabled instance started: %d", code)
	}
	tracks := scanNCM(t, c)
	if len(tracks) != 2 {
		t.Fatalf("multi-root scan omitted tracks: %+v", tracks)
	}
	tracks = scanProvider(t, c, "secondary", "secondary-scan-001")
	if len(tracks) != 3 {
		t.Fatalf("second instance replaced another library: %+v", tracks)
	}
	for _, name := range []string{"root-one.wav", "root-two.wav", "secondary.wav"} {
		track, ok := tracks[name]
		providerID := "local"
		if name == "secondary.wav" {
			providerID = "secondary"
		}
		if !ok || track.ProviderID != providerID {
			t.Fatalf("root changed provider identity: %+v", track)
		}
		request := playback.CreateRequest{SourceTrackID: track.ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}}}}
		code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", request, "yaml-play-"+name)
		var description playback.Description
		if code != 201 || json.Unmarshal(raw, &description) != nil {
			t.Fatalf("multi-root playback failed: %d %s", code, raw)
		}
		verifyRanges(t, c, description, payload)
	}
}
