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

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
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
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.plugin.source.localfiles", Executable: provider, Enabled: false, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "music", Name: "Music", Path: t.TempDir()}}}}}}}
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
	if _, err := os.Stat(filepath.Join(cfg.DataRoot, "providers", "b1548e96-3081-5c9c-8376-bf290e369d51")); !os.IsNotExist(err) {
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
				{PluginID: "musicfs.plugin.source.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Music", Path: music}}}}}},
				{PluginID: "musicfs.plugin.decoder.ffmpeg", Executable: audio, Enabled: test.plugin, Instances: []config.Instance{{InstanceID: "16b003e5-1bab-52bb-be10-0e83ec1c40ea", Enabled: test.instance, Config: config.InstanceConfig{FFmpegPath: ffmpeg, FFprobePath: ffprobe}}}},
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

func TestYAMLProviderSingleInstanceWithMultipleRoots(t *testing.T) {
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
	cfg, base := yamlServiceConfig(t)
	const id = "b1548e96-3081-5c9c-8376-bf290e369d51"
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.plugin.source.localfiles", Executable: provider, Enabled: true,
		Instances: []config.Instance{{InstanceID: id, Enabled: true, Config: config.InstanceConfig{MusicRoots: roots}}}}}
	// Even a disabled second instance violates the plugin declaration.
	duplicate := cfg
	duplicate.Plugins = append([]config.Plugin{}, cfg.Plugins...)
	duplicate.Plugins[0].Instances = append(append([]config.Instance{}, cfg.Plugins[0].Instances...), config.Instance{InstanceID: "c6007445-c664-49bd-b697-c8af9b606d04"})
	if _, err := config.Load(writeYAMLConfiguration(t, duplicate), map[string]string{"ADMIN_TOKEN": cfg.AdminToken}); err == nil {
		t.Fatal("local music accepted a second configured instance")
	}
	startNCMServer(t, cfg, base)
	c := login(t, base, cfg.AdminToken)
	code, raw, _ := c.request(t, "GET", "/api/v1/provider-instances", nil, "")
	var providers struct {
		Items []library.Provider `json:"items"`
	}
	if code != 200 || json.Unmarshal(raw, &providers) != nil || len(providers.Items) != 1 || providers.Items[0].ID != id || providers.Items[0].Availability != "available" {
		t.Fatalf("provider identity mismatch: %d %s", code, raw)
	}
	tracks := scanNCM(t, c)
	if len(tracks) != 2 {
		t.Fatalf("multi-root scan omitted tracks: %+v", tracks)
	}
	for _, name := range []string{"root-one.wav", "root-two.wav"} {
		track, ok := tracks[name]
		if !ok || track.ProviderID != id {
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
