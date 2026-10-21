package e2e_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestBundledPluginsAutostartPendingAndPersistedDisable(t *testing.T) {
	cfg, base := yamlServiceConfig(t)
	cfg.PluginBundle = build(t, "musicfs-plugins")
	path := writeYAMLConfiguration(t, cfg)
	stop := startAudioYAMLServer(t, path, cfg, base)
	c := login(t, base, cfg.AdminToken)
	check := func(ncmState string, restartRequired bool) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); ; {
			code, raw, _ := c.request(t, "GET", "/api/v1/admin/plugins", nil, "")
			var snapshot plugincontrol.Snapshot
			if code != 200 || json.Unmarshal(raw, &snapshot) != nil {
				t.Fatalf("plugins: %d %s", code, raw)
			}
			testkit.ValidateHTTP(t, "PluginManagementSnapshot", raw)
			valid := len(snapshot.Items) == 13 && snapshot.RestartRequired == restartRequired
			for _, p := range snapshot.Items {
				if p.Builtin != nil {
					valid = valid && p.Execution == "builtin" && !p.StateMutable && len(p.Instances) == 0
					continue
				}
				if p.Storage != nil {
					valid = valid && p.Required && !p.StateMutable && p.Storage.RuntimeState == "ready" && len(p.Instances) == 0
					continue
				}
				if p.DSP != nil {
					valid = valid && !p.StateMutable && p.DSP.ChainState == "not_configured" && len(p.Instances) == 0
					continue
				}
				want := "ready"
				if p.PluginID == "musicfs.plugin.source.localfiles" || p.PluginID == "musicfs.plugin.decoder.ffmpeg" {
					want = "pending_configuration"
				}
				if p.PluginID == "musicfs.plugin.decoder.ncm" {
					want = ncmState
					if restartRequired && len(p.Instances) == 1 {
						i := p.Instances[0]
						valid = valid && !p.ConfiguredEnabled && i.EffectiveEnabled && i.AcceptingNewWork && i.Reason != nil && *i.Reason == "RESTART_REQUIRED"
					}
				}
				valid = valid && len(p.Instances) == 1 && p.Instances[0].RuntimeState == want
				if len(p.Instances) == 1 && want == "pending_configuration" {
					i := p.Instances[0]
					valid = valid && !i.EffectiveEnabled && !i.AcceptingNewWork && i.Reason != nil && *i.Reason == "CONFIGURATION_REQUIRED"
				}
			}
			if valid {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("unexpected discovery states: %s", raw)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	check("ready", false)
	op := libraryManagementChange(t, c, "/api/v1/admin/plugins/musicfs.plugin.decoder.ncm/state", map[string]any{"enabled": false, "mode": "drain"})
	waitLibraryOperation(t, c, op.ID)
	if !op.RestartRequired {
		t.Fatal("saved plugin disable omitted restart requirement")
	}
	check("ready", true)
	stop()
	stop = startAudioYAMLServer(t, path, cfg, base)
	defer stop()
	c = login(t, base, cfg.AdminToken)
	check("disabled", false)
}
