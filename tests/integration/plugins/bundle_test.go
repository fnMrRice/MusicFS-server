package plugins_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
)

func TestUnifiedOnlineRolesAndProcessIsolation(t *testing.T) {
	path := onlineBinary(t, "./src/app/musicfs-plugins", "plugins with spaces")
	workers := []*pluginhost.OnlineWorker{}
	for _, role := range []string{"netease", "qqmusic", "applemusic"} {
		cfg := onlineConfig(t, path, role)
		cfg.Args = []string{"--plugin", role}
		workers = append(workers, onlineStart(t, cfg))
	}
	if err := workers[0].Close(); err != nil {
		t.Fatal(err)
	}
	// Closing a role must not kill the other processes sharing the same file.
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
	cfg := onlineConfig(t, path, "netease")
	cfg.Args = []string{"--plugin", "qqmusic"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if w, err := pluginhost.StartOnline(ctx, cfg); err == nil {
		w.Close()
		t.Fatal("accepted a different platform role")
	}
	for _, args := range [][]string{nil, {"--plugin", "unknown"}, {"--plugin", "local", "unexpected"}} {
		cmd := exec.CommandContext(ctx, path, args...)
		stdout, err := cmd.Output()
		if err == nil || len(stdout) != 0 {
			t.Fatalf("invalid arguments polluted protocol stdout: %q, %v", stdout, err)
		}
	}
}
