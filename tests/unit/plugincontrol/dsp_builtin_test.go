package plugincontrol_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestBuiltinDSPInventoryIsReadOnlyWithoutInstances(t *testing.T) {
	manager, err := plugincontrol.New(t.Context(), config.Config{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	for _, name := range []string{"equalizer", "compressor", "reverb"} {
		id := "musicfs.dsp." + name
		item := managedItem(t, manager, id)
		if item.StateMutable || item.SupportsMultipleInstances || item.ConfiguredEnabled || item.DSP == nil || item.DSP.Configured || item.DSP.ChainState != "disabled" || item.Instances == nil || len(item.Instances) != 0 || item.Execution != "builtin" || item.Version != bundledplugin.Version(id) {
			t.Fatalf("invalid builtin DSP declaration: %+v", item)
		}
		if item.Plugin.Validate(id) != nil || item.Plugin.LocalizedNames["zh-Hans"] == "" || item.Plugin.LocalizedNames["zh-Hant"] == "" || !reflect.DeepEqual(item.Plugin.Features, []string{"audio-dsp"}) {
			t.Fatal("invalid builtin DSP identity", item.Plugin)
		}
		for _, change := range []config.Change{
			{PluginID: id, Enabled: false},
			{PluginID: id, Enabled: true},
			{PluginID: id, InstanceID: "missing-instance", Settings: &config.InstanceSettings{}},
		} {
			var failure *domain.Error
			if _, err := manager.Change(t.Context(), "admin", change); !errors.As(err, &failure) || failure.Code != "UNSUPPORTED" {
				t.Fatal("builtin DSP mutation was not rejected", change, err)
			}
		}
		raw, err := json.Marshal(item)
		if err != nil {
			t.Fatal(err)
		}
		testkit.ValidateHTTP(t, "ManagedPlugin", raw)
	}
}

func TestBuiltinDSPProjectionKeepsAliasesPositionsAndParameterIsolation(t *testing.T) {
	chain := []audio.NodeSpec{{Name: "gain", Version: 1}}
	for _, name := range []string{"equalizer", "compressor", "reverb"} {
		chain = append(chain, audio.NodeSpec{Name: name, Version: 1, Params: map[string]float64{"testValue": 0.5}},
			audio.NodeSpec{Name: "bypass", Version: 1}, audio.NodeSpec{Name: "musicfs.dsp." + name, Version: 1})
	}
	manager, err := plugincontrol.New(t.Context(), config.Config{AudioDSP: config.AudioDSPConfig{Enabled: true, Chain: chain}}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	for index, name := range []string{"equalizer", "compressor", "reverb"} {
		id := "musicfs.dsp." + name
		item := managedItem(t, manager, id)
		if !item.ConfiguredEnabled || !item.DSP.Configured || item.DSP.ChainState != "in_chain" || len(item.DSP.Nodes) != 2 {
			t.Fatal("missing configured builtin DSP nodes", item)
		}
		nodes := item.DSP.Nodes
		if nodes[0].Name != name || nodes[0].Position != index*3+2 || nodes[1].Name != id || nodes[1].Position != index*3+4 || nodes[0].Mode != "builtin" || nodes[1].Mode != "builtin" || nodes[0].Version != 1 || nodes[1].Params == nil {
			t.Fatal("builtin DSP projection lost original chain metadata", nodes)
		}
		nodes[0].Params["testValue"] = 999
		if managedItem(t, manager, id).DSP.Nodes[0].Params["testValue"] != 0.5 {
			t.Fatal("snapshot changed startup DSP parameters")
		}
	}
}
