package e2e_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/lifecycle"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"go.yaml.in/yaml/v3"
)

// This optional check executes the supplied installation artifact, never a
// replacement binary built by the test. Default regression reports it skipped.
func TestActualInstallationBundleScanAudioWebAndExit(t *testing.T) {
	bundle := os.Getenv("MUSICFS_RELEASE_ROOT")
	if bundle == "" {
		t.Skip("set MUSICFS_RELEASE_ROOT to a completed installation bundle")
	}
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Fatal("installation audio validation requires FFmpeg and ffprobe")
	}
	installed := copyInstallation(t, bundle)
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	server := filepath.Join(installed, "bin", "musicfs"+suffix)
	token, err := exec.Command(server, "token").Output()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(installed, "configs", "server.example.yaml"), map[string]string{"ADMIN_TOKEN": strings.TrimSpace(string(token))})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range cfg.Plugins {
		if filepath.Base(p.Executable) != "musicfs-plugins"+suffix || len(p.Args) != 2 || p.Args[0] != "--plugin" {
			t.Fatal("installation does not use the shared plugin executable")
		}
		if p.PluginID == "musicfs.plugin.source.localfiles" {
			for _, instance := range p.Instances {
				if len(instance.Config.MusicRoots) != 0 || !instance.PendingConfiguration {
					t.Fatal("installation must start without selected import directories")
				}
			}
		}
	}
	music := filepath.Join(installed, "music")
	if err = os.Mkdir(music, 0700); err != nil {
		t.Fatal(err)
	}
	payloads := map[string][]byte{}
	for _, name := range []string{"markers.wav", "lossless.flac"} {
		payloads[name], err = os.ReadFile(filepath.Join("..", "testdata", "audio", name))
		if err != nil {
			t.Fatal(err)
		}
	}
	mp3, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "vbr-untagged.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	payloads["sample.ncm"], err = testkit.BuildNCM(mp3, map[string]any{"format": "mp3"})
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range payloads {
		if err = os.WriteFile(filepath.Join(music, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.Listen = listener.Addr().String()
	listener.Close()
	base := "http://" + cfg.Listen
	cfg.AllowedOrigins = []string{base}
	for i := range cfg.Plugins {
		p := &cfg.Plugins[i]
		switch p.PluginID {
		case "musicfs.plugin.source.localfiles", "musicfs.plugin.decoder.ncm", "musicfs.scraper.local":
			p.Enabled = true
			for j := range p.Instances {
				p.Instances[j].Enabled = true
				if p.PluginID == "musicfs.plugin.source.localfiles" {
					p.Instances[j].Permissions = &config.Permissions{ReadDirectories: []string{music}}
					p.Instances[j].Config.MusicRoots = []config.MusicRoot{{ID: "local", Name: "Installation fixtures", Path: music}}
				}
			}
		case "musicfs.plugin.decoder.ffmpeg":
			p.Enabled = true
			p.Instances[0].Enabled = true
			p.Instances[0].Config = config.InstanceConfig{FFmpegPath: ffmpeg, FFprobePath: ffprobe}
		}
	}
	raw, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(installed, "configs", "server.yaml")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, server, "serve", "--config", path, "--startup-mode", "client-managed")
	cmd.Dir = installed
	for _, value := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(value), "MUSICFS_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "MUSICFS_ADMIN_TOKEN="+cfg.AdminToken)
	log, err := os.Create(filepath.Join(installed, "validation.log"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = log.Close() })
	cmd.Stdout = log
	cmd.Stderr = log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	finished := false
	t.Cleanup(func() {
		if !finished {
			select {
			case <-done:
			case <-time.After(40 * time.Second):
				cancel()
				<-done
			}
		}
	})
	probe := http.Client{Timeout: time.Second}
	for deadline := time.Now().Add(25 * time.Second); ; {
		response, e := probe.Get(base + "/api/v1/health/ready")
		if e == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				break
			}
		}
		select {
		case e := <-done:
			finished = true
			t.Fatalf("installed server startup failed: %v", e)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("installed server startup timed out")
		}
		time.Sleep(20 * time.Millisecond)
	}
	c := login(t, base, cfg.AdminToken)
	status := lifecycleStatus(t, c)
	lease := acquireLifecycle(t, c, status.InstanceID, "installation-check")
	// Logout on an assertion failure still lets the owned server clean up workers.
	t.Cleanup(func() {
		if !finished {
			_, _, _ = c.request(t, "DELETE", "/api/v1/auth/session", nil, "")
		}
	})
	tracks := scanNCM(t, c)
	if len(tracks) != 3 {
		t.Fatalf("installed scan: %d tracks", len(tracks))
	}
	for _, name := range []string{"markers.wav", "sample.ncm"} {
		req := playback.CreateRequest{SourceTrackID: tracks[name].ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}, {MIME: "audio/mpeg", CanPlay: "probably"}}}}
		code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", req, domain.NewID())
		var d playback.Description
		if code != 201 || json.Unmarshal(raw, &d) != nil {
			t.Fatalf("installed raw playback: %d %s", code, raw)
		}
		want := payloads[name]
		if name == "sample.ncm" {
			want = mp3
		}
		verifyRanges(t, c, d, want)
	}
	first := createYAMLProcessedPlayback(t, c, tracks["lossless.flac"].ID, "installed-processing")
	pcm := fetchYAMLAudioPCM(t, c, ffmpeg, first.StreamURL)
	assertYAMLAudioDuration(t, pcm, 6000)
	assertYAMLAudioTone(t, pcm, 4300, 5300, 660)
	code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions/"+first.SessionID+"/seek", playback.ControlRequest{AttemptID: first.AttemptID, PositionMs: 4000}, "installed-seek")
	var seek playback.Description
	if code != 200 || json.Unmarshal(raw, &seek) != nil {
		t.Fatalf("installed seek: %d %s", code, raw)
	}
	pcm = fetchYAMLAudioPCM(t, c, ffmpeg, seek.StreamURL)
	assertYAMLAudioDuration(t, pcm, 2000)
	assertYAMLAudioTone(t, pcm, 250, 1250, 660)
	response, err := probe.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	page, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || !bytes.Contains(page, []byte("<html")) {
		t.Fatal("installed Web artifact was not served")
	}
	for name, want := range payloads {
		got, e := os.ReadFile(filepath.Join(music, name))
		if e != nil || !bytes.Equal(got, want) {
			t.Fatal("installation validation modified music", name)
		}
	}
	code, raw, _ = c.request(t, "POST", "/api/v1/lifecycle/leases/"+lease.ID+"/release", lifecycle.ReleaseRequest{InstanceID: status.InstanceID}, "")
	if code != 200 {
		t.Fatalf("installed release lease: %d %s", code, raw)
	}
	select {
	case err = <-done:
		finished = true
		if err != nil {
			t.Fatal("installed server did not exit cleanly", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("installed server did not stop after its last lease")
	}
	t.Log("Installed Server and official workers: WAV/NCM Range, decoded FLAC PCM/seek, Web and clean process exit passed")
}

func copyInstallation(t *testing.T, bundle string) string {
	t.Helper()
	if _, err := os.Stat(filepath.Join(bundle, ".incomplete")); !os.IsNotExist(err) {
		t.Fatal("bundle is incomplete")
	}
	sums, err := os.ReadFile(filepath.Join(bundle, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(sums)), "\n") {
		digest, name, ok := strings.Cut(line, "  ")
		if !ok || !filepath.IsLocal(filepath.FromSlash(name)) {
			t.Fatal("invalid checksum path")
		}
		f, e := os.Open(filepath.Join(bundle, filepath.FromSlash(name)))
		if e != nil {
			t.Fatal(e)
		}
		hash := sha256.New()
		_, e = io.Copy(hash, f)
		f.Close()
		if e != nil || hex.EncodeToString(hash.Sum(nil)) != digest {
			t.Fatal("bundle checksum mismatch", name)
		}
	}
	installed := t.TempDir()
	err = filepath.WalkDir(bundle, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(bundle, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(installed, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		defer input.Close()
		mode := os.FileMode(0600)
		if strings.HasPrefix(filepath.ToSlash(rel), "bin/") {
			mode = 0700
		}
		output, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		_, err = io.Copy(output, input)
		closeErr := output.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
	if err != nil {
		t.Fatal(err)
	}
	return installed
}
