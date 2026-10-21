package plugincontrol

import (
	"maps"

	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

func dspPlugin(id string) bool {
	switch id {
	case "musicfs.dsp.gain", "musicfs.dsp.equalizer", "musicfs.dsp.compressor", "musicfs.dsp.reverb":
		return true
	}
	return false
}

// 直接内置的 DSP 只投影启动链，不注册普通实例或外部进程。
func (m *Manager) builtinDSPPluginsLocked() []Plugin {
	result := make([]Plugin, 0, 3)
	for _, name := range []string{"equalizer", "compressor", "reverb"} {
		id := "musicfs.dsp." + name
		status := &DSPStatus{ChainState: "not_configured", Nodes: []DSPNode{}}
		for index, node := range m.desired.AudioDSP.Chain {
			if node.Name != name && node.Name != id {
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
		result = append(result, Plugin{PluginID: id, Plugin: pluginconfig.Identity(id), Version: bundledplugin.Version(id), Execution: "builtin",
			ConfiguredEnabled: status.Configured, DSP: status, Instances: []Instance{}})
	}
	return result
}
