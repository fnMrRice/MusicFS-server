package plugins_test

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
)

func TestUnifiedOnlinePluginSelectsIsolatedPlatformInstances(t *testing.T) {
	path := onlineBinary(t, "github.com/fnMrRice/MusicFS-server/plugins/source/online/cmd/musicfs-plugin-online", "unified online with spaces")
	workers := make([]*pluginhost.OnlineWorker, 0, 3)
	for _, platform := range []string{online.Netease, online.QQMusic, online.AppleMusic} {
		cfg := onlineConfig(t, path, platform)
		cfg.Args = []string{"--plugin", platform}
		w := onlineStart(t, cfg)
		workers = append(workers, w)
		if d := w.Descriptor(); d.Platform != platform || d.Validate(platform) != nil {
			t.Fatal("unified worker selected a different platform", platform, d)
		}
		onlineExpect(t, w.Resolve(context.Background(), "42"), "PLATFORM_AUTH_REQUIRED")
	}
	if err := workers[0].Close(); err != nil {
		t.Fatal(err)
	}
	for _, w := range workers[1:] {
		if _, err := w.Describe(context.Background()); err != nil {
			t.Fatal("closing one unified instance affected another platform", err)
		}
	}
	cfg := onlineConfig(t, path, online.Netease)
	cfg.Args = []string{"--plugin", online.QQMusic}
	if wrong, err := pluginhost.StartOnline(context.Background(), cfg); err == nil {
		_ = wrong.Close()
		t.Fatal("unified worker accepted a mismatched platform scope")
	} else {
		onlineExpect(t, err, "PROTOCOL_MISMATCH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, args := range [][]string{nil, {"--plugin"}, {"--plugin", "unknown"}, {"--plugin", "netease", "extra"}, {"--plugin", "qqmusic", "--plugin", "netease"}, {"--platform", "netease"}} {
		output, err := exec.CommandContext(ctx, path, args...).Output()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 2 || len(output) != 0 {
			t.Fatalf("invalid selector did not fail cleanly: %v, stdout=%q, err=%v", args, output, err)
		}
	}
}
