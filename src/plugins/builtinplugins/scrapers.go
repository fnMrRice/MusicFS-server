package builtinplugins

import (
	"net/http"
	"os"

	"github.com/fnMrRice/MusicFS-server/plugins/scraper/online"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

func init() {
	for _, p := range online.Catalog(http.DefaultClient, os.Getenv("MUSICFS_ACOUSTID_API_KEY")) {
		d := p.Descriptor()
		_, configurable := p.(scraper.SettingsProvider)
		identity := &pluginproto.PluginIdentity{ID: d.PluginID, Name: d.Name, LocalizedNames: map[string]string{}, Features: []string{"metadata", d.Type}}
		if err := plugincontrol.RegisterBuiltin(plugincontrol.BuiltinDeclaration{PluginID: d.PluginID, Identity: identity, Version: bundledplugin.Version(d.PluginID), Capabilities: d.Fields, SettingsAvailable: configurable, Configured: func(config.Config) bool { return d.Available }}); err != nil {
			panic(err)
		}
	}
}
