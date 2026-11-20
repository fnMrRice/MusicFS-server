package e2e_test

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library/recommendations"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
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
	cfg.Plugins = []config.Plugin{onlinePlugin(online.Netease, program, "7fe223d0-a3bd-5e87-87f8-305dc26d9041"), onlinePlugin(online.QQMusic, build(t, "musicfs-plugin-qqmusic"), "b5e86bbe-524b-53fc-bde9-bbe3cbb02a80"), onlinePlugin(online.AppleMusic, build(t, "musicfs-plugin-applemusic"), "2e2b7962-89d2-5815-9804-0d395497f906")}
	cfg.Plugins[0].Instances[0].Name = "  display fallback  "
	cfg.Plugins[0].Instances = append(cfg.Plugins[0].Instances, config.Instance{InstanceID: "7a202030-76d0-5655-b70e-aa3833cc17de"})
	t.Run("grouped-read-cache-refresh", func(t *testing.T) {
		path := writeYAMLConfiguration(t, cfg)
		stop := startAudioYAMLServer(t, path, cfg, base)
		c := login(t, base, cfg.AdminToken)
		authenticateManagedOnline(t, c, "7fe223d0-a3bd-5e87-87f8-305dc26d9041")
		first := albumRequest[recommendations.Response](t, c, "GET", "/recommendations?limit=1", nil, "", 200, "Recommendations")
		if len(first.Sources) != 4 {
			t.Fatalf("grouped sources: %+v", first)
		}
		byID := make(map[string]recommendations.Source, len(first.Sources))
		for _, source := range first.Sources {
			byID[source.InstanceID] = source
		}
		initial := byID["7fe223d0-a3bd-5e87-87f8-305dc26d9041"]
		if initial.Error != nil || initial.Name != "Fixture listener" || len(initial.Items) != 1 || initial.LastSuccessAt == nil {
			t.Fatalf("account recommendations: %+v", initial)
		}
		for id, expected := range map[string]struct{ code, name string }{
			"7a202030-76d0-5655-b70e-aa3833cc17de": {"PLUGIN_DISABLED", "NetEase Cloud Music"},
			"b5e86bbe-524b-53fc-bde9-bbe3cbb02a80": {"PLATFORM_AUTH_REQUIRED", "QQ Music"},
			"2e2b7962-89d2-5815-9804-0d395497f906": {"PLATFORM_AUTH_REQUIRED", "Apple Music"},
		} {
			source, ok := byID[id]
			if !ok || source.Error == nil || source.Error.Code != expected.code || len(source.Items) != 0 || source.LastSuccessAt != nil || source.Name != expected.name {
				t.Fatalf("source failure or alias lost: %+v", source)
			}
		}
		cached := albumRequest[recommendations.Response](t, c, "GET", "/recommendations?instanceId=7fe223d0-a3bd-5e87-87f8-305dc26d9041&limit=1", nil, "", 200, "Recommendations").Sources[0]
		if cached.Error != nil || cached.Name != "Fixture listener" || !cached.LastSuccessAt.Equal(*initial.LastSuccessAt) {
			t.Fatal("cache hit contacted rate-limited worker")
		}
		stale := albumRequest[recommendations.Response](t, c, "GET", "/recommendations?instanceId=7fe223d0-a3bd-5e87-87f8-305dc26d9041&limit=1&refresh=true", nil, "", 200, "Recommendations").Sources[0]
		if stale.Error == nil || stale.Error.Code != "RATE_LIMITED" || !stale.Stale || len(stale.Items) != 1 || !stale.LastSuccessAt.Equal(*cached.LastSuccessAt) {
			t.Fatalf("failed refresh did not retain previous data: %+v", stale)
		}
		held := albumRequest[recommendations.Response](t, c, "GET", "/recommendations?instanceId=7fe223d0-a3bd-5e87-87f8-305dc26d9041&limit=1&refresh=true", nil, "", 200, "Recommendations").Sources[0]
		if held.Error == nil || held.Error.RetryAfter == nil || !held.Stale {
			t.Fatal("explicit refresh bypassed cooldown")
		}
		time.Sleep(2100 * time.Millisecond)
		latest := albumRequest[recommendations.Response](t, c, "GET", "/recommendations?instanceId=7fe223d0-a3bd-5e87-87f8-305dc26d9041&limit=1&refresh=true", nil, "", 200, "Recommendations")
		recovered := latest.Sources[0]
		if recovered.Error != nil || recovered.Stale || !recovered.LastSuccessAt.After(*cached.LastSuccessAt) || latest.HistoryDepth != 1 {
			t.Fatal("recommendation recovery failed")
		}
		prior := albumRequest[recommendations.Response](t, c, "GET", "/recommendations?instanceId=7fe223d0-a3bd-5e87-87f8-305dc26d9041&limit=1&history=1", nil, "", 200, "Recommendations").Sources[0]
		if prior.Error != nil || prior.LastSuccessAt == nil || !prior.LastSuccessAt.Equal(*cached.LastSuccessAt) {
			t.Fatal("recommendation history did not return the previous successful page")
		}
		stop()
		startAudioYAMLServer(t, path, cfg, base)
		c = login(t, base, cfg.AdminToken)
		authenticateManagedOnline(t, c, "7fe223d0-a3bd-5e87-87f8-305dc26d9041")
		restored := albumRequest[recommendations.Response](t, c, "GET", "/recommendations?instanceId=7fe223d0-a3bd-5e87-87f8-305dc26d9041&limit=1", nil, "", 200, "Recommendations")
		if restored.HistoryDepth != 1 || restored.Sources[0].LastSuccessAt == nil || !restored.Sources[0].LastSuccessAt.Equal(*recovered.LastSuccessAt) {
			t.Fatal("restart lost the saved recommendation or fetched a new one")
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
			source := albumRequest[recommendations.Response](t, c, "GET", "/recommendations?instanceId=7fe223d0-a3bd-5e87-87f8-305dc26d9041&limit=1", nil, "", 200, "Recommendations").Sources[0]
			if source.Error == nil || source.Error.Code != expected || len(source.Items) != 0 {
				t.Fatalf("expected %s for unavailable runtime: %+v", expected, source)
			}
			code, raw, _ := c.request(t, "GET", "/api/v1/online-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/search?q=Fixture", nil, "")
			var body struct{ Error struct{ Code string } }
			if code != 503 || json.Unmarshal(raw, &body) != nil || body.Error.Code != expected {
				t.Fatalf("online host disagreed with recommendations: %d %s", code, raw)
			}
		}
		check("SOURCE_OFFLINE")
		disabled := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/state", map[string]any{"enabled": false})
		waitLibraryOperation(t, c, disabled.ID)
		if !disabled.RestartRequired {
			t.Fatal("saved disable omitted restart requirement")
		}
		check("SOURCE_OFFLINE")
		enabled := libraryManagementChange(t, c, "/api/v1/admin/plugin-instances/7fe223d0-a3bd-5e87-87f8-305dc26d9041/state", map[string]any{"enabled": true})
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
