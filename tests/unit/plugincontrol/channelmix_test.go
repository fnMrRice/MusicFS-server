package plugincontrol_test

import (
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

func TestChannelConversionIsAnImmutableBuiltinAlgorithm(t *testing.T) {
	manager, err := plugincontrol.New(t.Context(), config.Config{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	id := "musicfs.plugin.decoder.channelmix"
	item := managedItem(t, manager, id)
	if item.StateMutable || item.SupportsMultipleInstances || item.Execution != "builtin" || item.Version != bundledplugin.ChannelMixVersion || item.Builtin == nil || !item.Builtin.Configured || !item.ConfiguredEnabled || item.Instances == nil || len(item.Instances) != 0 {
		t.Fatal("invalid channel converter declaration", item)
	}
	if !reflect.DeepEqual(item.Builtin.Capabilities, []string{"audio.channels.stereo", "audio.channels.5.1", "audio.channels.7.1"}) {
		t.Fatal("channel converter capabilities differ", item.Builtin.Capabilities)
	}
	for _, enabled := range []bool{false, true} {
		if _, err := manager.Change(t.Context(), "admin", config.Change{PluginID: id, Enabled: enabled}); err == nil {
			t.Fatal("channel converter accepted ordinary plugin state control")
		}
	}
}
