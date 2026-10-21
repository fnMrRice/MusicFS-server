package plugincontrol

import (
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

// BuiltinStatus 只描述已装配能力与业务开关，不宣称依赖可用或当前正在处理任务。
type BuiltinStatus struct {
	Configured   bool     `json:"configured"`
	Capabilities []string `json:"capabilities"`
}

func algorithmPlugin(id string) bool {
	switch id {
	case "musicfs.plugin.analysis.chromaprint", "musicfs.plugin.tags.flac", "musicfs.plugin.matching.standard", "musicfs.plugin.transform.thumbnail":
		return true
	}
	return false
}

func (m *Manager) builtinPluginsLocked() []Plugin {
	items := []struct {
		id           string
		configured   bool
		capabilities []string
	}{
		{"musicfs.plugin.analysis.chromaprint", m.desired.Fingerprinting.Enabled, []string{"fingerprint.chromaprint"}},
		{"musicfs.plugin.tags.flac", m.desired.WriteBack.Enabled, []string{"tags.flac.rewrite"}},
		{"musicfs.plugin.matching.standard", true, []string{"matching.candidates"}},
		{"musicfs.plugin.transform.thumbnail", true, []string{"artwork.thumbnail256"}},
	}
	result := make([]Plugin, 0, len(items))
	for _, item := range items {
		result = append(result, Plugin{SettingsAvailable: item.id == "musicfs.plugin.matching.standard", PluginID: item.id, Plugin: pluginconfig.Identity(item.id), Version: bundledplugin.Version(item.id), Execution: "builtin", ConfiguredEnabled: item.configured,
			Builtin: &BuiltinStatus{Configured: item.configured, Capabilities: item.capabilities}, Instances: []Instance{}})
	}
	return result
}
