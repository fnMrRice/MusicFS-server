package builtinplugins

import (
	"github.com/fnMrRice/MusicFS-server/plugins/content/asmr"
	"github.com/fnMrRice/MusicFS-server/plugins/content/cdalbum"
	"github.com/fnMrRice/MusicFS-server/plugins/content/radio"
	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

// ContentTypes 按作品库注册顺序返回本次构建装配的内容类型。
func ContentTypes() []contenttype.Descriptor {
	return []contenttype.Descriptor{cdalbum.Descriptor(), asmr.Descriptor(), radio.Descriptor()}
}

func init() {
	for _, d := range ContentTypes() {
		identity := &pluginproto.PluginIdentity{ID: d.PluginID, Name: d.Name, LocalizedNames: map[string]string{}, Features: []string{"content-type"}}
		if err := plugincontrol.RegisterBuiltin(plugincontrol.BuiltinDeclaration{
			PluginID: d.PluginID, Identity: identity, Version: d.PluginVersion,
			Capabilities: d.Views, Configured: func(config.Config) bool { return true },
		}); err != nil {
			panic(err)
		}
	}
}
