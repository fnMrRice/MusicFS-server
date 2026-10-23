package plugincontrol

import (
	"maps"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

func dspPlugin(id string) bool {
	for _, descriptor := range audio.Catalog() {
		if descriptor.PluginID == id {
			return true
		}
	}
	return false
}

// 直接内置的 DSP 只投影启动链，不注册普通实例或外部进程。
func (m *Manager) builtinDSPPluginsLocked() []Plugin {
	result := []Plugin{}
	seen := map[string]bool{}
	for _, descriptor := range audio.Catalog() {
		id := descriptor.PluginID
		if id == "" || descriptor.PluginVersion == "" || descriptor.Capability == "gain" || descriptor.Capability == "channelConversion" || seen[id] {
			continue
		}
		seen[id] = true
		status := &DSPStatus{ChainState: "not_configured", Nodes: []DSPNode{}}
		for index, node := range m.desired.AudioDSP.Chain {
			selected, ok := audio.Definition(node.Name)
			if !ok || selected.PluginID != id {
				continue
			}
			params := maps.Clone(node.Params)
			if params == nil {
				params = map[string]float64{}
			}
			status.Nodes = append(status.Nodes, DSPNode{Position: index + 1, Name: node.Name, Version: node.Version, Params: params, Mode: "builtin"})
		}
		status.Configured = len(status.Nodes) != 0
		if status.Configured {
			status.ChainState = "in_chain"
		}
		if !m.desired.AudioDSP.Enabled {
			status.ChainState = "disabled"
		}
		result = append(result, Plugin{PluginID: id, Plugin: dspIdentity(descriptor), Version: descriptor.PluginVersion, Execution: "builtin",
			ConfiguredEnabled: m.desired.AudioDSP.Enabled && status.Configured, DSP: status, Instances: []Instance{}})
	}
	return result
}
