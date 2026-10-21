package plugincontrol_test

import (
	"encoding/json"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestInfrastructureHTTPInventoryAndStateProtection(t *testing.T) {
	h := setup(t)
	server, cookies := managementHTTP(t, h.manager)
	w := managementRequest(server, cookies["admin"], "GET", "/admin/plugins", "", "", "")
	requireHTTP(t, w, 200, "")
	testkit.ValidateHTTP(t, "PluginManagementSnapshot", w.Body.Bytes())
	revision := w.Header().Get("ETag")
	var snapshot plugincontrol.Snapshot
	if err := json.Unmarshal(w.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, item := range snapshot.Items {
		if item.Storage == nil && item.DSP == nil && item.Builtin == nil {
			if !item.StateMutable {
				t.Fatal("ordinary plugin lost state control", item.PluginID)
			}
			continue
		}
		seen[item.PluginID] = true
		if item.StateMutable || len(item.Instances) != 0 {
			t.Fatal("infrastructure exposed optional instance control", item)
		}
		if item.Storage != nil && (!item.Required || item.Storage.RuntimeState != "ready") {
			t.Fatal("database was not marked required and ready", item)
		}
		for _, body := range []string{`{"enabled":false}`, `{"enabled":false,"mode":"force"}`, `{"enabled":true}`} {
			result := managementRequest(server, cookies["admin"], "PUT", "/admin/plugins/"+item.PluginID+"/state", body, revision, "protected-"+item.PluginID)
			requireHTTP(t, result, 415, "UNSUPPORTED")
		}
		result := managementRequest(server, cookies["admin"], "GET", "/admin/plugins/"+item.PluginID+"/instances", "", "", "")
		requireHTTP(t, result, 200, "")
		testkit.ValidateHTTP(t, "ManagedPluginInstances", result.Body.Bytes())
	}
	if !seen["musicfs.plugin.storage.sqlite"] || !seen["musicfs.dsp.gain"] {
		t.Fatal("missing infrastructure inventory", seen)
	}
	for _, id := range []string{"musicfs.dsp.equalizer", "musicfs.dsp.compressor", "musicfs.dsp.reverb"} {
		if !seen[id] {
			t.Fatal("missing builtin DSP inventory", id)
		}
	}
	for _, id := range []string{"musicfs.plugin.analysis.chromaprint", "musicfs.plugin.tags.flac", "musicfs.plugin.matching.standard", "musicfs.plugin.transform.thumbnail", "musicfs.plugin.decoder.channelmix"} {
		if !seen[id] {
			t.Fatal("missing builtin algorithm inventory", id)
		}
	}
	if h.manager.Snapshot().Revision != snapshot.Revision || h.manager.Snapshot().RestartRequired {
		t.Fatal("rejected infrastructure changes modified persisted state")
	}
}
