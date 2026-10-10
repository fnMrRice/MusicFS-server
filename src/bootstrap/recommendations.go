package bootstrap

import (
	"github.com/fnMrRice/MusicFS-server/src/onlinehost"
	"github.com/fnMrRice/MusicFS-server/src/recommendations"
)

func configureRecommendations(runtimes []onlinehost.Runtime) (*recommendations.Service, error) {
	items := make([]recommendations.Runtime, 0, len(runtimes))
	for _, runtime := range runtimes {
		item := recommendations.Runtime{Owner: runtime.Owner, InstanceID: runtime.InstanceID, Name: runtime.Name, Platform: runtime.Platform, AccountID: "", ConfigRevision: runtime.ConfigRevision}
		if runtime.Enabled {
			item.Provider = runtime.Provider
		}
		items = append(items, item)
	}
	return recommendations.New(items, recommendations.Config{})
}
