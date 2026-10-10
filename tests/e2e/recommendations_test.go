package e2e_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/recommendations"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestRecommendationsActualServerPartialFailuresCacheAndRestart(t *testing.T) {
	program := filepath.Join(t.TempDir(), "netfixture")
	if runtime.GOOS == "windows" {
		program += ".exe"
	}
	command := exec.Command("go", "build", "-mod=readonly", "-o", program, "./tests/testkit/online-worker")
	command.Dir = testkit.RepositoryRoot()
	if raw, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build recommendation fixture: %v %s", err, raw)
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{onlinePlugin(online.Netease, program, "net"), onlinePlugin(online.QQMusic, build(t, "musicfs-plugin-qqmusic"), "qq"), onlinePlugin(online.AppleMusic, build(t, "musicfs-plugin-applemusic"), "apple")}
	cfg.Plugins[0].Instances[0].Name = "  display fallback  "
	cfg.Plugins[0].Instances = append(cfg.Plugins[0].Instances, config.Instance{InstanceID: "offline", Role: "provider"})
	t.Run("grouped-read-cache-refresh", func(t *testing.T) {
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		first := albumRequest[recommendations.Response](t, c, "GET", "/recommendations?limit=1", nil, "", 200, "Recommendations")
		if len(first.Sources) != 4 || first.Sources[0].Error != nil || first.Sources[0].Name != "net" || len(first.Sources[0].Items) != 1 {
			t.Fatalf("grouped sources: %+v", first)
		}
		for i, code := range []string{"SOURCE_OFFLINE", "UNSUPPORTED", "AUTH_REQUIRED"} {
			if first.Sources[i+1].Error == nil || first.Sources[i+1].Error.Code != code || len(first.Sources[i+1].Items) != 0 || first.Sources[i+1].LastSuccessAt != nil {
				t.Fatalf("source failure lost: %+v", first.Sources[i+1])
			}
		}
		cached := albumRequest[recommendations.Response](t, c, "GET", "/recommendations?instanceId=net&limit=1", nil, "", 200, "Recommendations").Sources[0]
		if cached.Error != nil || !cached.LastSuccessAt.Equal(*first.Sources[0].LastSuccessAt) {
			t.Fatal("cache hit contacted rate-limited worker")
		}
		stale := albumRequest[recommendations.Response](t, c, "GET", "/recommendations?instanceId=net&limit=1&refresh=true", nil, "", 200, "Recommendations").Sources[0]
		if stale.Error == nil || stale.Error.Code != "RATE_LIMITED" || !stale.Stale || len(stale.Items) != 1 || !stale.LastSuccessAt.Equal(*cached.LastSuccessAt) {
			t.Fatalf("failed refresh did not retain previous data: %+v", stale)
		}
		held := albumRequest[recommendations.Response](t, c, "GET", "/recommendations?instanceId=net&limit=1&refresh=true", nil, "", 200, "Recommendations").Sources[0]
		if held.Error == nil || held.Error.RetryAfter == nil || !held.Stale {
			t.Fatal("explicit refresh bypassed cooldown")
		}
		time.Sleep(2100 * time.Millisecond)
		recovered := albumRequest[recommendations.Response](t, c, "GET", "/recommendations?instanceId=net&limit=1&refresh=true", nil, "", 200, "Recommendations").Sources[0]
		if recovered.Error != nil || recovered.Stale || !recovered.LastSuccessAt.After(*cached.LastSuccessAt) {
			t.Fatal("recommendation recovery failed")
		}
		assertOnlineDoesNotImport(t, c)
	})
	t.Run("disabled-restart-clears-cache", func(t *testing.T) {
		for i := range cfg.Plugins {
			cfg.Plugins[i].Enabled = false
		}
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		response := albumRequest[recommendations.Response](t, c, "GET", "/recommendations?limit=1", nil, "", 200, "Recommendations")
		for _, source := range response.Sources {
			if source.Error == nil || source.Error.Code != "SOURCE_OFFLINE" || source.LastSuccessAt != nil || len(source.Items) != 0 {
				t.Fatal("disabled configuration reused old recommendation cache")
			}
		}
		assertOnlineDoesNotImport(t, c)
	})
}
