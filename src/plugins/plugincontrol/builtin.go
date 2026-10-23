package plugincontrol

import (
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

// BuiltinStatus 只描述已装配能力与业务开关，不宣称依赖可用或当前正在处理任务。
type BuiltinStatus struct {
	Configured   bool     `json:"configured"`
	Capabilities []string `json:"capabilities"`
}

func (m *Manager) algorithmPlugin(id string) bool {
	for _, item := range m.builtins {
		if item.PluginID == id {
			return true
		}
	}
	for _, descriptor := range audio.Catalog() {
		if descriptor.Capability == "channelConversion" && descriptor.PluginID == id {
			return true
		}
	}
	return false
}

func (m *Manager) builtinPluginsLocked() []Plugin {
	result := make([]Plugin, 0, len(m.builtins))
	for _, item := range m.builtins {
		configured := item.Configured(m.desired)
		result = append(result, Plugin{SettingsAvailable: item.SettingsAvailable, PluginID: item.PluginID, Plugin: item.Identity.Clone(), Version: item.Version, Execution: "builtin", ConfiguredEnabled: configured,
			Builtin: &BuiltinStatus{Configured: configured, Capabilities: append([]string(nil), item.Capabilities...)}, Instances: []Instance{}})
	}
	seen := map[string]bool{}
	for _, descriptor := range audio.Catalog() {
		if descriptor.Capability != "channelConversion" || descriptor.PluginID == "" || seen[descriptor.PluginID] {
			continue
		}
		seen[descriptor.PluginID] = true
		capabilities := []string{}
		for _, layout := range descriptor.Output.Layouts {
			capabilities = append(capabilities, "audio.channels."+layout)
		}
		result = append(result, Plugin{PluginID: descriptor.PluginID, Plugin: dspIdentity(descriptor), Version: descriptor.PluginVersion, Execution: "builtin", ConfiguredEnabled: m.desired.AudioChannels.Enabled,
			Builtin: &BuiltinStatus{Configured: m.desired.AudioChannels.Enabled, Capabilities: capabilities}, Instances: []Instance{}})
	}
	return result
}
