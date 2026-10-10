package e2e_test

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
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
		authenticateManagedOnline(t, c, "net")
		first := albumRequest[recommendations.Response](t, c, "GET", "/recommendations?limit=1", nil, "", 200, "Recommendations")
		if len(first.Sources) != 4 || first.Sources[0].Error != nil || first.Sources[0].Name != "net" || len(first.Sources[0].Items) != 1 {
			t.Fatalf("grouped sources: %+v", first)
		}
		for i, code := range []string{"PLUGIN_DISABLED", "PLATFORM_AUTH_REQUIRED", "PLATFORM_AUTH_REQUIRED"} {
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
			if source.Error == nil || source.Error.Code != "PLUGIN_DISABLED" || source.LastSuccessAt != nil || len(source.Items) != 0 {
				t.Fatal("disabled configuration reused old recommendation cache")
			}
		}
		assertOnlineDoesNotImport(t, c)
	})
	t.Run("enabled-worker-failure-is-offline", func(t *testing.T) {
		cfg.Plugins[0].Enabled = true
		cfg.Plugins[0].Executable = filepath.Join(t.TempDir(), "missing-online-worker")
		stop := startAudioYAMLServer(t, writeYAMLConfiguration(t, cfg), cfg, base)
		defer stop()
		c := login(t, base, cfg.AdminToken)
		check := func(expected string) {
			t.Helper()
			source := albumRequest[recommendations.Response](t, c, "GET", "/recommendations?instanceId=net&limit=1", nil, "", 200, "Recommendations").Sources[0]
			if source.Error == nil || source.Error.Code != expected || len(source.Items) != 0 {
				t.Fatalf("expected %s for unavailable runtime: %+v", expected, source)
			}
			code, raw, _ := c.request(t, "GET", "/api/v1/online-instances/net/search?q=Fixture", nil, "")
			var body struct{ Error struct{ Code string } }
			if code != 503 || json.Unmarshal(raw, &body) != nil || body.Error.Code != expected {
				t.Fatalf("online host disagreed with recommendations: %d %s", code, raw)
			}
		}
		check("SOURCE_OFFLINE")
		disabled := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/net/state", map[string]any{"enabled": false})
		waitLibraryOperation(t, c, disabled.ID)
		check("PLUGIN_DISABLED")
		enabled := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/net/state", map[string]any{"enabled": true})
		for deadline := time.Now().Add(3 * time.Second); ; {
			code, raw, _ := c.request(t, "GET", "/api/v1/admin/plugin-operations/"+enabled.ID, nil, "")
			var operation plugincontrol.Operation
			if code != 200 || json.Unmarshal(raw, &operation) != nil {
				t.Fatalf("failed-start operation unavailable: %d %s", code, raw)
			}
			if operation.Status == "failed" {
				break
			}
			if operation.Status == "succeeded" || time.Now().After(deadline) {
				t.Fatal("missing worker was incorrectly enabled", operation)
			}
			time.Sleep(20 * time.Millisecond)
		}
		check("SOURCE_OFFLINE")
	})
}
