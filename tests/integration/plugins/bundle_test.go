package plugins_test

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
)

func TestIndependentOnlinePluginsAndProcessIsolation(t *testing.T) {
	programs := map[string]string{}
	workers := []*pluginhost.OnlineWorker{}
	for _, role := range []string{"netease", "qqmusic", "applemusic"} {
		path := onlineBinary(t, "github.com/fnMrRice/MusicFS-server/plugins/source/online/cmd/musicfs-plugin-"+role, role+" plugin with spaces")
		programs[role] = path
		cfg := onlineConfig(t, path, role)
		workers = append(workers, onlineStart(t, cfg))
	}
	if err := workers[0].Close(); err != nil {
		t.Fatal(err)
	}
	// Closing one independently built plugin must not stop the other instances.
	for _, w := range workers[1:] {
		select {
		case <-w.Done():
			t.Fatal("closing one role terminated another role")
		default:
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	path := onlineBinary(t, "./src/app/musicfs-plugins", "local plugins with spaces")
	cfg := onlineConfig(t, programs["qqmusic"], "netease")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if w, err := pluginhost.StartOnline(ctx, cfg); err == nil {
		w.Close()
		t.Fatal("accepted a different platform role")
	}
	for _, standalone := range programs {
		cmd := exec.CommandContext(ctx, standalone, "--plugin", "netease")
		stdout, err := cmd.Output()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 2 || len(stdout) != 0 {
			t.Fatalf("standalone plugin accepted role arguments: %q, %v", stdout, err)
		}
	}
	for _, args := range [][]string{nil, {"--plugin", "unknown"}, {"--plugin", "local", "unexpected"}, {"--plugin", "netease"}, {"--plugin", "qqmusic"}, {"--plugin", "applemusic"}} {
		cmd := exec.CommandContext(ctx, path, args...)
		stdout, err := cmd.Output()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 2 || len(stdout) != 0 {
			t.Fatalf("invalid arguments polluted protocol stdout: %q, %v", stdout, err)
		}
	}
}
