package e2e_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/app/bootstrap"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestNCMServerRoundTripAndDisabledPlugin(t *testing.T) {
	provider := build(t, "musicfs-plugin-local")
	decoder := build(t, "musicfs-plugin-ncm")
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "vbr-untagged.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	const cacheBudget = 2 << 20
	encrypted, err := testkit.BuildNCMWithOptions(payload, map[string]any{"musicName": "中文解密曲目", "format": "flac", "duration": 1}, testkit.NCMOptions{Cover: []byte{1, 2, 3, 4}, CoverPadding: cacheBudget})
	if err != nil {
		t.Fatal(err)
	}
	if len(encrypted) <= cacheBudget || len(payload) >= cacheBudget {
		t.Fatal("NCM fixture does not isolate container and representation cache sizes")
	}
	oversized, err := testkit.BuildNCM(bytes.Repeat(payload, cacheBudget/len(payload)+1), nil)
	if err != nil {
		t.Fatal(err)
	}
	wave, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	for _, enabled := range []bool{true, false} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			music := filepath.Join(dir, "music")
			if err := os.MkdirAll(music, 0700); err != nil {
				t.Fatal(err)
			}
			for name, data := range map[string][]byte{"中文 source.ncm": encrypted, "broken.ncm": encrypted[:21], "oversized.ncm": oversized, "plain.wav": wave} {
				if err := os.WriteFile(filepath.Join(music, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := listener.Addr().String()
			listener.Close()
			base := "http://" + address
			cfg := config.Config{ConfigVersion: 1, Listen: address, AllowLoopbackHTTP: true, AllowedOrigins: []string{base}, AdminToken: domain.NewID(), DataRoot: filepath.Join(dir, "data"), MediaRoot: filepath.Join(dir, "media"), CacheRoot: filepath.Join(dir, "cache"), MaxSessions: 8, MaxProcessing: 2, MaxCacheBytes: cacheBudget, SessionTTLSeconds: 3600,
				Hashing: config.HashingConfig{MaxConcurrent: 1, BytesPerSecond: 4 << 20},
				Plugins: []config.Plugin{
					{PluginID: "musicfs.plugin.source.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Name: "NCM test", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "NCM test", Path: music}}}}}},
					{PluginID: "musicfs.plugin.decoder.ncm", Executable: decoder, Enabled: enabled, Instances: []config.Instance{{InstanceID: "96412f52-9ff4-5a82-9eaa-7644a5837cda", Enabled: true}}},
				}}
			disableLocalLyricsImport(&cfg)
			startNCMServer(t, cfg, base)
			c := login(t, base, cfg.AdminToken)
			tracks := scanNCM(t, c)
			if len(tracks) != 4 {
				t.Fatalf("expected four source files, got %d", len(tracks))
			}
			request := playback.CreateRequest{SourceTrackID: tracks["中文 source.ncm"].ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/mpeg", CanPlay: "probably"}, {MIME: "audio/wav", CanPlay: "probably"}}}}
			code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", request, "ncm-play-0001")
			if enabled {
				if code != 201 {
					t.Fatalf("create NCM: %d %s", code, raw)
				}
				testkit.ValidateHTTP(t, "Playback", raw)
				var description playback.Description
				if err := json.Unmarshal(raw, &description); err != nil {
					t.Fatal(err)
				}
				track := tracks["中文 source.ncm"]
				if track.Container == nil || *track.Container != "ncm" || track.Codec == nil || *track.Codec != "mp3" || track.DurationMs != nil || track.Title == nil || *track.Title != "中文解密曲目" {
					t.Fatalf("unverified envelope metadata leaked into format/duration: %+v", track)
				}
				if description.DeliveryMode != "proxy" || description.SeekMode != "byteRange" || description.OutputProfile.MIME != "audio/mpeg" || len(description.EffectiveProcessing) != 1 || description.EffectiveProcessing[0].Type != "ncmDecrypt" {
					t.Fatalf("unexpected NCM playback description: %s", raw)
				}
				verifyRanges(t, c, description, payload)
				other := login(t, base, cfg.AdminToken)
				if status, _, _ := other.request(t, "GET", description.StreamURL, nil, ""); status != 404 {
					t.Fatalf("NCM stream leaked to another session: %d", status)
				}
				code, replay, _ := c.request(t, "POST", "/api/v1/playback/sessions", request, "ncm-play-0001")
				if code != 201 || !bytes.Equal(raw, replay) {
					t.Fatal("NCM create did not replay exactly")
				}
				actual, err := os.ReadFile(filepath.Join(music, "中文 source.ncm"))
				if err != nil || sha256.Sum256(actual) != sha256.Sum256(encrypted) {
					t.Fatal("original NCM source changed")
				}
				if err := os.WriteFile(filepath.Join(music, "中文 source.ncm"), []byte("changed by test"), 0600); err != nil {
					t.Fatal(err)
				}
				verifyRanges(t, c, description, payload)
				if status, _, _ := c.request(t, "POST", "/api/v1/playback/sessions", request, "ncm-changed-0001"); status != 409 {
					t.Fatalf("changed encrypted source accepted: %d", status)
				}
				if code, _, _ := c.request(t, "DELETE", "/api/v1/playback/sessions/"+description.SessionID, nil, ""); code != 204 {
					t.Fatalf("NCM stop failed: %d", code)
				}
				if code, _, _ := c.request(t, "GET", description.StreamURL, nil, ""); code != 404 {
					t.Fatalf("stopped NCM stream still available: %d", code)
				}
			} else if code != 415 {
				t.Fatalf("NCM accepted without plugin: %d %s", code, raw)
			}
			request.SourceTrackID = tracks["broken.ncm"].ID
			if code, _, _ := c.request(t, "POST", "/api/v1/playback/sessions", request, "ncm-broken-0001"); code != 415 {
				t.Fatalf("broken NCM accepted: %d", code)
			}
			if enabled {
				request.SourceTrackID = tracks["oversized.ncm"].ID
				if code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", request, "ncm-budget-0001"); code != http.StatusTooManyRequests || !bytes.Contains(raw, []byte("RATE_LIMITED")) {
					t.Fatalf("oversized decrypted representation accepted: %d %s", code, raw)
				}
			}
			request.SourceTrackID = tracks["plain.wav"].ID
			if code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", request, "plain-play-0001"); code != 201 {
				t.Fatalf("NCM failure affected plain WAV: %d %s", code, raw)
			}
		})
	}
}

func startNCMServer(t *testing.T, cfg config.Config, base string) {
	t.Helper()
	loaded, err := config.Load(writeYAMLConfiguration(t, cfg), map[string]string{"ADMIN_TOKEN": cfg.AdminToken})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- bootstrap.Serve(ctx, loaded, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(8 * time.Second):
			t.Error("NCM server shutdown timeout")
		}
	})
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
		select {
		case err := <-done:
			t.Fatalf("NCM server startup failed: %v", err)
		default:
		}
		response, err := http.Get(base + "/api/v1/health/ready")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("NCM server startup timeout")
}

func scanNCM(t *testing.T, c *client) map[string]library.Track {
	t.Helper()
	return scanProvider(t, c, "b1548e96-3081-5c9c-8376-bf290e369d51", "ncm-scan-0001")
}

func scanProvider(t *testing.T, c *client, provider, key string) map[string]library.Track {
	t.Helper()
	code, raw, _ := c.request(t, "POST", "/api/v1/provider-instances/"+provider+"/scan-jobs", providerScanRequest(t, c, provider), key)
	var accepted struct {
		JobID string `json:"jobId"`
	}
	if code != 202 || json.Unmarshal(raw, &accepted) != nil {
		t.Fatalf("NCM scan submission: %d %s", code, raw)
	}
	for deadline := time.Now().Add(15 * time.Second); ; {
		_, raw, _ = c.request(t, "GET", "/api/v1/scan-jobs/"+accepted.JobID, nil, "")
		var job struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(raw, &job) != nil {
			t.Fatalf("invalid scan result: %s", raw)
		}
		if job.Status == "succeeded" {
			break
		}
		if job.Status == "failed" || time.Now().After(deadline) {
			t.Fatalf("NCM scan failed: %s", raw)
		}
		time.Sleep(20 * time.Millisecond)
	}
	code, raw, _ = c.request(t, "GET", "/api/v1/source-tracks", nil, "")
	var page library.Page
	if code != 200 || json.Unmarshal(raw, &page) != nil {
		t.Fatalf("NCM list failed: %d %s", code, raw)
	}
	result := map[string]library.Track{}
	for _, track := range page.Items {
		result[track.DisplayName] = track
	}
	return result
}
