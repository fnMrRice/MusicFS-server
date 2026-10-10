package plugins_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
)

func onlineBinary(t *testing.T, pkg, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	cmd := exec.Command("go", "build", "-mod=readonly", "-o", path, pkg)
	cmd.Dir = filepath.Join("..", "..", "..")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build online worker: %v %s", err, out)
	}
	return path
}
func onlineConfig(t *testing.T, path, platform string) pluginhost.OnlineConfig {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return pluginhost.OnlineConfig{Executable: path, SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), StateDir: t.TempDir(), Platform: platform, UserID: "owner", InstanceID: "online-instance", Timeout: 3 * time.Second}
}
func onlineExpect(t *testing.T, err error, code string) {
	t.Helper()
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}
func onlineStart(t *testing.T, cfg pluginhost.OnlineConfig) *pluginhost.OnlineWorker {
	t.Helper()
	w, err := pluginhost.StartOnline(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	return w
}

func TestActualOnlineWorkerMappingScopeAndCleanup(t *testing.T) {
	path := onlineBinary(t, "./tests/testkit/online-worker", "online-fixture")
	cfg := onlineConfig(t, path, online.Netease)
	w := onlineStart(t, cfg)
	page, err := w.Search(context.Background(), online.SearchRequest{Query: "fixture", Limit: 1})
	if err != nil || len(page.Tracks) != 1 || page.Tracks[0].ExternalID != "42" {
		t.Fatalf("search: %+v %v", page, err)
	}
	time.Sleep(1010 * time.Millisecond)
	track, err := w.Metadata(context.Background(), "42")
	if err != nil || track.ExternalID != "42" {
		t.Fatalf("metadata: %+v %v", track, err)
	}
	time.Sleep(1010 * time.Millisecond)
	recs, err := w.PublicRecommendations(context.Background(), 1)
	if err != nil || len(recs.Items) != 1 || recs.Personalized {
		t.Fatalf("recommendations: %+v %v", recs, err)
	}
	time.Sleep(1010 * time.Millisecond)
	playlist, err := w.ObservePlaylist(context.Background(), "91")
	if err != nil || !playlist.Complete || playlist.Consistency != online.Unversioned || len(playlist.Entries) != 3 || playlist.Entries[0].ExternalTrackID != playlist.Entries[1].ExternalTrackID || playlist.Entries[1].Position != 1 {
		t.Fatalf("playlist: %+v %v", playlist, err)
	}
	onlineExpect(t, w.Resolve(context.Background(), "42"), "UNSUPPORTED")
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.Done():
	default:
		t.Fatal("process cleanup was not completed")
	}
	entries, err := os.ReadDir(cfg.StateDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("working directory was not reclaimed: %v", err)
	}
	_, err = w.Search(context.Background(), online.SearchRequest{Query: "fixture", Limit: 1})
	onlineExpect(t, err, "SOURCE_OFFLINE")
}

func TestActualOnlineWorkerCancellationAndInvalidResults(t *testing.T) {
	path := onlineBinary(t, "./tests/testkit/online-worker", "online-fixture")
	cfg := onlineConfig(t, path, online.Netease)
	w := onlineStart(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := w.Search(ctx, online.SearchRequest{Query: "slow", Limit: 1})
	if !errors.Is(err, context.DeadlineExceeded) {
		onlineExpect(t, err, "CANCELED")
	}
	if _, err = w.Describe(context.Background()); err != nil {
		t.Fatalf("cancellation broke the control channel: %v", err)
	}
	_ = w.Close()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"bad-scope", "bad-url"} {
		malicious := filepath.Join(t.TempDir(), mode+filepath.Ext(path))
		if err = os.WriteFile(malicious, body, 0700); err != nil {
			t.Fatal(err)
		}
		bad := onlineStart(t, onlineConfig(t, malicious, online.Netease))
		_, err = bad.Search(context.Background(), online.SearchRequest{Query: "fixture", Limit: 1})
		onlineExpect(t, err, "PROTOCOL_MISMATCH")
		_ = bad.Close()
	}
}

func TestActualOnlineEntrypointsAndUnsupportedPlatforms(t *testing.T) {
	for _, platform := range []string{online.Netease, online.QQMusic, online.AppleMusic} {
		t.Run(platform, func(t *testing.T) {
			path := onlineBinary(t, "./src/app/musicfs-plugin-"+platform, "online-"+platform)
			cfg := onlineConfig(t, path, platform)
			w := onlineStart(t, cfg)
			if d := w.Descriptor(); d.Platform != platform || d.Validate(platform) != nil {
				t.Fatal("descriptor mismatch")
			}
			onlineExpect(t, w.Resolve(context.Background(), "42"), "UNSUPPORTED")
			if platform == online.AppleMusic {
				_, err := w.Search(context.Background(), online.SearchRequest{Query: "fixture", Limit: 1})
				onlineExpect(t, err, "AUTH_REQUIRED")
			}
			if platform == online.QQMusic {
				_, err := w.Metadata(context.Background(), "SyntheticMID")
				onlineExpect(t, err, "UNSUPPORTED")
			}
			_ = w.Close()
			cfg.Platform = online.AppleMusic
			if platform == online.AppleMusic {
				cfg.Platform = online.Netease
			}
			if wrong, err := pluginhost.StartOnline(context.Background(), cfg); err == nil {
				wrong.Close()
				t.Fatal("another platform executable was accepted")
			}
		})
	}
}

func TestOnlineDisabledAndChecksumRejectBeforeStart(t *testing.T) {
	cfg := pluginhost.OnlineConfig{Executable: filepath.Join(t.TempDir(), "missing.exe"), SHA256: strings.Repeat("0", 64), StateDir: filepath.Join(t.TempDir(), "must-not-exist"), Platform: online.Netease, UserID: "owner", InstanceID: "instance", Disabled: true}
	if w, err := pluginhost.StartOnline(context.Background(), cfg); err == nil {
		w.Close()
		t.Fatal("disabled instance started")
	}
	if _, err := os.Stat(cfg.StateDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("disabled instance created runtime state")
	}
	path := onlineBinary(t, "./tests/testkit/online-worker", "qqfixture")
	cfg = onlineConfig(t, path, online.QQMusic)
	cfg.SHA256 = strings.Repeat("0", 64)
	if w, err := pluginhost.StartOnline(context.Background(), cfg); err == nil {
		w.Close()
		t.Fatal("untrusted executable started")
	}
	cfg = onlineConfig(t, path, online.QQMusic)
	w := onlineStart(t, cfg)
	_, err := w.Search(context.Background(), online.SearchRequest{Query: "fixture", Limit: 1})
	onlineExpect(t, err, "TEMPORARY_FAILURE")
}
