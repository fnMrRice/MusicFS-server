package plugincontrol_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/src/audio"
	"github.com/fnMrRice/MusicFS-server/src/audio/dspworker"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
)

type infrastructureRepository struct {
	plugincontrol.Repository
	version   string
	available bool
}

func (*infrastructureRepository) RecoverPluginOperations(context.Context) error { return nil }
func (r *infrastructureRepository) PluginVersion() string                       { return r.version }
func (r *infrastructureRepository) Available() bool                             { return r.available }

func managedItem(t *testing.T, manager *plugincontrol.Manager, id string) plugincontrol.Plugin {
	t.Helper()
	for _, item := range manager.Snapshot().Items {
		if item.PluginID == id {
			return item
		}
	}
	t.Fatal("missing managed plugin", id)
	return plugincontrol.Plugin{}
}

func TestInfrastructureIsListedWithoutOptionalInstances(t *testing.T) {
	manager, err := plugincontrol.New(t.Context(), config.Config{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	storage := managedItem(t, manager, "musicfs.plugin.storage.sqlite")
	if !storage.Required || storage.StateMutable || !storage.ConfiguredEnabled || storage.Version != bundledplugin.SQLiteVersion || storage.Storage.RuntimeState != "unknown" || storage.Storage.Mode != "builtin" || len(storage.Instances) != 0 {
		t.Fatalf("invalid required storage declaration: %+v", storage)
	}
	gain := managedItem(t, manager, "musicfs.dsp.gain")
	if gain.StateMutable || gain.ConfiguredEnabled || gain.DSP.Configured || gain.DSP.ChainState != "not_configured" || gain.Version != bundledplugin.GainVersion || len(gain.Instances) != 0 {
		t.Fatalf("invalid unconfigured DSP declaration: %+v", gain)
	}
	for _, item := range []plugincontrol.Plugin{storage, gain} {
		if item.Plugin.Validate(item.PluginID) != nil {
			t.Fatal("invalid infrastructure identity", item.Plugin)
		}
		for _, enabled := range []bool{false, true} {
			if _, err := manager.Change(t.Context(), "admin", config.Change{PluginID: item.PluginID, Enabled: enabled}); err == nil || !strings.Contains(err.Error(), "startup configuration") {
				t.Fatal("infrastructure state change was not rejected", err)
			}
		}
	}
}

func TestDSPConfigurationProjectionKeepsChainPositionsAndExecutionModes(t *testing.T) {
	registered := dspworker.Config{Name: "musicfs.dsp.gain", Executable: "private-dsp-path", SHA256: "private-dsp-sha", Args: []string{"private-dsp-argument"}}
	cfg := config.Config{AudioDSP: config.AudioDSPConfig{Plugins: []dspworker.Config{registered}}}
	manager, err := plugincontrol.New(t.Context(), cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	status := managedItem(t, manager, "musicfs.dsp.gain").DSP
	if !status.Configured || status.ChainState != "not_in_chain" || len(status.Nodes) != 0 {
		t.Fatal("registered DSP was incorrectly described as active", status)
	}
	cfg.AudioDSP.Chain = []audio.NodeSpec{
		{Name: "vendor.equalizer", Version: 1},
		{Name: "gain", Version: 1, Params: map[string]float64{"gainDB": -3}},
		{Name: "bypass", Version: 1},
		{Name: "musicfs.dsp.gain", Version: 1, Params: map[string]float64{"gainDB": -6}},
		{Name: "gain", Version: 1},
	}
	manager, err = plugincontrol.New(t.Context(), cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	gain := managedItem(t, manager, "musicfs.dsp.gain")
	status = gain.DSP
	if !gain.ConfiguredEnabled || !status.Configured || status.ChainState != "in_chain" || len(status.Nodes) != 3 {
		t.Fatal("missing configured DSP nodes", status)
	}
	if status.Nodes[0].Position != 2 || status.Nodes[0].Mode != "builtin" || status.Nodes[1].Position != 4 || status.Nodes[1].Mode != "process" || status.Nodes[2].Position != 5 || status.Nodes[2].Params == nil {
		t.Fatal("DSP projection lost original positions, modes or empty parameter object", status.Nodes)
	}
	status.Nodes[0].Params["gainDB"] = 999
	if managedItem(t, manager, "musicfs.dsp.gain").DSP.Nodes[0].Params["gainDB"] != -3 {
		t.Fatal("snapshot modified startup DSP parameters")
	}
	raw, err := json.Marshal(manager.SnapshotForScope(t.Context(), "another-user", false))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-dsp-path", "private-dsp-sha", "private-dsp-argument", "vendor.equalizer"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("snapshot exposed deployment authority or attributed third-party nodes to official gain", secret)
		}
	}
}

func TestExternalStorageUsesVerifiedVersionAndMemoryAvailability(t *testing.T) {
	repo := &infrastructureRepository{version: "3.2.1-rc.1", available: true}
	cfg := config.Config{Storage: config.StorageConfig{Backend: "plugin", Executable: "private-db-path", SHA256: "private-db-sha", Args: []string{"private-db-argument"}, Options: map[string]string{"token": "private-db-token"}}}
	manager, err := plugincontrol.New(t.Context(), cfg, nil, repo, "9.9.9-adapter")
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	item := managedItem(t, manager, "musicfs.plugin.storage.plugin")
	if item.Plugin.Name != "External Database" || item.Version != repo.version || !item.Required || item.StateMutable || item.Storage.RuntimeState != "ready" || item.Storage.Mode != "process" {
		t.Fatal("external database metadata guessed its identity or release", item)
	}
	repo.available = false
	if managedItem(t, manager, item.PluginID).Storage.RuntimeState != "unavailable" {
		t.Fatal("storage health did not reflect host-local availability")
	}
	raw, err := json.Marshal(manager.Snapshot())
	if err != nil || strings.Contains(string(raw), "private-db-") {
		t.Fatal("storage projection exposed deployment authority", string(raw), err)
	}
	repo.version = ""
	if unexpected, err := plugincontrol.New(t.Context(), cfg, nil, repo, "9.9.9-adapter"); err == nil {
		unexpected.Close()
		t.Fatal("transport adapter version was used as an external backend release")
	}
}

func TestDSPVerifiedVersionOverridesBundledDeclaration(t *testing.T) {
	const instanceID = "16b003e5-1bab-52bb-be10-0e83ec1c40ea"
	cfg := config.Config{Plugins: []config.Plugin{{PluginID: "musicfs.plugin.decoder.ffmpeg", Instances: []config.Instance{{InstanceID: instanceID}}}}}
	manager, err := plugincontrol.New(t.Context(), cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	version := "2.0.0-rc.1"
	if err = manager.Register(instanceID, plugincontrol.Hooks{DSPVersion: func(name string) string {
		if name != "musicfs.dsp.gain" {
			t.Fatal("unexpected DSP version query", name)
		}
		return version
	}}, false); err != nil {
		t.Fatal(err)
	}
	if managedItem(t, manager, "musicfs.dsp.gain").Version != version {
		t.Fatal("verified DSP release was lost")
	}
	version = "invalid"
	if managedItem(t, manager, "musicfs.dsp.gain").Version != bundledplugin.GainVersion {
		t.Fatal("invalid DSP release replaced the official declaration")
	}
}
