// Package builtinplugins 声明默认构建装配的内置插件。
package builtinplugins

import (
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

func init() {
	always := func(config.Config) bool { return true }
	for _, item := range []struct {
		id, capability string
		settings       bool
		configured     func(config.Config) bool
	}{
		{"musicfs.plugin.analysis.chromaprint", "fingerprint.chromaprint", false, func(c config.Config) bool { return c.Fingerprinting.Enabled }},
		{"musicfs.plugin.tags.flac", "tags.flac.rewrite", false, func(c config.Config) bool { return c.WriteBack.Enabled }},
		{"musicfs.plugin.matching.standard", "matching.candidates", true, always},
		{"musicfs.plugin.transform.thumbnail", "artwork.thumbnail256", false, always},
	} {
		if err := plugincontrol.RegisterBuiltin(plugincontrol.BuiltinDeclaration{PluginID: item.id, Identity: pluginconfig.Identity(item.id), Version: bundledplugin.Version(item.id), Capabilities: []string{item.capability}, SettingsAvailable: item.settings, Configured: item.configured}); err != nil {
			panic(err)
		}
	}
}
