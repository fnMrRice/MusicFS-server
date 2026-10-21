package pluginhost_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

var fixtureBinary, fixtureSHA string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "musicfs-host-fixture-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fixtureBinary = filepath.Join(dir, "host-worker.exe")
	cmd := exec.Command("go", "build", "-mod=readonly", "-o", fixtureBinary, "./tests/testapps/host-worker")
	cmd.Dir = testkit.RepositoryRoot()
	out, err := cmd.CombinedOutput()
	code := 1
	if err != nil {
		fmt.Fprintf(os.Stderr, "Build host fixture: %v: %s\n", err, out)
	} else {
		body, readErr := os.ReadFile(fixtureBinary)
		if readErr != nil {
			fmt.Fprintln(os.Stderr, readErr)
		} else {
			fixtureSHA = fmt.Sprintf("%x", sha256.Sum256(body))
			code = m.Run()
		}
	}
	os.RemoveAll(dir)
	os.Exit(code)
}

type fixtureControl struct {
	View    *online.AccountView `json:"view"`
	Count   json.RawMessage     `json:"count,omitempty"`
	Failure string              `json:"failure"`
	Started string              `json:"started"`
	Release string              `json:"release"`
}

func setControl(t *testing.T, dir string, c fixtureControl) {
	t.Helper()
	body, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "control.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
}

func onlineConfig(t *testing.T) (pluginhost.OnlineConfig, string) {
	t.Helper()
	dir := t.TempDir()
	return pluginhost.OnlineConfig{Executable: fixtureBinary, SHA256: fixtureSHA, Args: []string{"online", dir}, StateDir: t.TempDir(), Platform: online.Netease, UserID: "admin", InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Timeout: 3 * time.Second}, dir
}

func startOnline(t *testing.T) (*pluginhost.OnlineWorker, string) {
	t.Helper()
	cfg, dir := onlineConfig(t)
	w, err := pluginhost.StartOnline(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	return w, dir
}

func localConfig(t *testing.T, mode string) pluginhost.Config {
	t.Helper()
	return pluginhost.Config{Executable: fixtureBinary, SHA256: fixtureSHA, Args: []string{mode, t.TempDir()}, StateDir: t.TempDir(), DatabasePath: filepath.Join(t.TempDir(), "plugins.sqlite"), UserID: "admin", InstanceID: "local",
		Roots: []sourceaccess.RootConfig{{ID: "one", Path: t.TempDir()}, {ID: "two", Path: t.TempDir()}}}
}

func startLocal(t *testing.T, cfg pluginhost.Config) *pluginhost.Worker {
	t.Helper()
	w, err := pluginhost.Start(t.Context(), cfg)
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

func waitFile(t *testing.T, path string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("fixture request did not reach barrier")
		case <-time.After(time.Millisecond):
		}
	}
}
