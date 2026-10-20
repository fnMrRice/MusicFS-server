package pluginproto

import "github.com/fnMrRice/MusicFS-server/sdk/online"

type OnlineEntityPage struct {
	Context Context `json:"context"`
	online.EntityPageRequest
}
