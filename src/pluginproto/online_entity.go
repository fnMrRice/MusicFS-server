package pluginproto

import "github.com/fnMrRice/MusicFS-server/src/online"

type OnlineEntityPage struct {
	Context Context `json:"context"`
	online.EntityPageRequest
}
