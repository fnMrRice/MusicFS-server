package pluginproto

import "github.com/fnMrRice/MusicFS-server/sdk/online"

const OnlineAccountRevision = "online-account-v1"

type OnlineAccount struct {
	Context Context               `json:"context"`
	Request online.AccountRequest `json:"request"`
}
