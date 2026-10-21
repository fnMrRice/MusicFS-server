package plugincontrol

import (
	"maps"

	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

// Infrastructure metadata is a safe projection of startup settings. It never
// grants access to process arguments, paths, checksums or storage options.
type StorageStatus struct {
	Backend      string `json:"backend"`
	Mode         string `json:"mode"`
	RuntimeState string `json:"runtimeState"`
}

type DSPStatus struct {
	Configured bool      `json:"configured"`
	ChainState string    `json:"chainState"`
	Nodes      []DSPNode `json:"nodes"`
}

type DSPNode struct {
	Position int                `json:"position"`
	Name     string             `json:"name"`
	Version  int                `json:"version"`
	Params   map[string]float64 `json:"params"`
	Mode     string             `json:"mode"`
}

func (m *Manager) storageBackend() string {
	if m.desired.Storage.Backend == "" {
		return "sqlite"
	}
	return m.desired.Storage.Backend
}

func (m *Manager) storagePluginID() string {
	return "musicfs.plugin.storage." + m.storageBackend()
}

// A supplied declaration comes from the selected registered driver. External
// workers must supply their verified release; the transport adapter's version
// is deliberately ignored. All metadata reads are host-local and non-blocking.
func (m *Manager) initializeStorage(declarations []string) error {
	backend := m.storageBackend()
	if backend == "sqlite" {
		m.storageVersion = bundledplugin.SQLiteVersion
	} else if backend != "plugin" && len(declarations) != 0 {
		m.storageVersion = declarations[0]
	}
	if versioned, ok := m.repo.(interface{ PluginVersion() string }); ok {
		if version := versioned.PluginVersion(); pluginproto.ValidPluginVersion(version) {
			m.storageVersion = version
		}
	}
	if !pluginproto.ValidPluginVersion(m.storageVersion) {
		return domain.Fail("INVALID_ARGUMENT", "Storage plugin release version is required")
	}
	return nil
}

func (m *Manager) storagePluginLocked() Plugin {
	backend := m.storageBackend()
	status := &StorageStatus{Backend: backend, Mode: "builtin", RuntimeState: "unknown"}
	if backend == "plugin" {
		status.Mode = "process"
	}
	if m.repo != nil {
		status.RuntimeState = "ready"
	}
	if availability, ok := m.repo.(interface{ Available() bool }); ok && !availability.Available() {
		status.RuntimeState = "unavailable"
	}
	id := m.storagePluginID()
	identity := pluginconfig.Identity(id)
	if identity == nil {
		identity = &pluginproto.PluginIdentity{ID: id, Name: "Database (" + backend + ")", LocalizedNames: map[string]string{"zh": "数据库（" + backend + "）"}, Features: []string{"database"}}
		if backend == "plugin" {
			identity.Name = "External Database"
			identity.LocalizedNames = map[string]string{"zh": "外部数据库", "zh-Hans": "外部数据库", "zh-Hant": "外部資料庫"}
		}
	}
	return Plugin{PluginID: id, Plugin: identity, Version: m.storageVersion, Required: true, ConfiguredEnabled: true, Storage: status, Instances: []Instance{}}
}

// Built-in gain and the official gain worker use the same package. Each node
// retains its execution mode and original position in the complete chain.
// This describes configuration, never a currently running playback worker.
func (m *Manager) gainPluginLocked() Plugin {
	status := &DSPStatus{ChainState: "not_configured", Nodes: []DSPNode{}}
	for _, registered := range m.desired.AudioDSP.Plugins {
		if registered.Name == "musicfs.dsp.gain" {
			status.Configured = true
		}
	}
	for index, node := range m.desired.AudioDSP.Chain {
		mode := "builtin"
		if node.Name == "musicfs.dsp.gain" {
			mode = "process"
		} else if node.Name != "gain" {
			continue
		}
		params := maps.Clone(node.Params)
		if params == nil {
			params = map[string]float64{}
		}
		status.Nodes = append(status.Nodes, DSPNode{Position: index + 1, Name: node.Name, Version: node.Version, Params: params, Mode: mode})
		status.Configured = true
	}
	if status.Configured {
		status.ChainState = "not_in_chain"
	}
	if len(status.Nodes) != 0 {
		status.ChainState = "in_chain"
	}
	version := bundledplugin.GainVersion
	for _, runtime := range m.runtimes {
		if runtime.hooks.DSPVersion != nil {
			if verified := runtime.hooks.DSPVersion("musicfs.dsp.gain"); pluginproto.ValidPluginVersion(verified) {
				version = verified
				break
			}
		}
	}
	return Plugin{PluginID: "musicfs.dsp.gain", Plugin: pluginconfig.Identity("musicfs.dsp.gain"), Version: version,
		ConfiguredEnabled: len(status.Nodes) != 0, DSP: status, Instances: []Instance{}}
}
