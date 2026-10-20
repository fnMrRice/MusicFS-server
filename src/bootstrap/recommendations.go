package bootstrap

import (
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/onlinehost"
	"github.com/fnMrRice/MusicFS-server/src/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/recommendations"
)

func configureRecommendations(runtimes []onlinehost.Runtime) (*recommendations.Service, error) {
	items := make([]recommendations.Runtime, 0, len(runtimes))
	for _, runtime := range runtimes {
		identity := pluginconfig.Identity(pluginproto.OnlinePluginID(runtime.Platform))
		if provider, ok := runtime.Provider.(interface {
			PluginIdentity() *pluginproto.PluginIdentity
		}); ok {
			if verified := provider.PluginIdentity(); verified != nil {
				identity = verified
			}
		}
		name := ""
		if identity != nil {
			name = identity.Name
		}
		item := recommendations.Runtime{Owner: runtime.Owner, InstanceID: runtime.InstanceID, Name: name, Platform: runtime.Platform, AccountID: "", ConfigRevision: runtime.ConfigRevision}
		if runtime.Enabled {
			item.Provider = runtime.Provider
		}
		items = append(items, item)
	}
	return recommendations.New(items, recommendations.Config{})
}
