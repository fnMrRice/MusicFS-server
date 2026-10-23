package e2e_test

import (
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
)

// disableLocalLyricsImport 仅由不验证自动歌词导入的测试显式调用，保留生产环境的依赖校验。
func disableLocalLyricsImport(cfg *config.Config) {
	for p := range cfg.Plugins {
		plugin := &cfg.Plugins[p]
		if plugin.PluginID != "musicfs.plugin.source.localfiles" {
			continue
		}
		for i := range plugin.Instances {
			instance := &plugin.Instances[i]
			if instance.ImportDefaults == nil {
				instance.ImportDefaults = &pluginproto.ImportOverrides{}
			}
			disabled := false
			instance.ImportDefaults.ImportLyrics = &disabled
		}
	}
}
