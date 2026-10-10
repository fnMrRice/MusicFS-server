package pluginproto

import "github.com/fnMrRice/MusicFS-server/src/online"

type OnlineTrackBatch struct {
	Context     Context  `json:"context"`
	ExternalIDs []string `json:"externalIds"`
}
type OnlinePlaylistTracks struct {
	Context Context `json:"context"`
	online.PlaylistTracksRequest
}

type OnlinePlaylists struct {
	Context Context `json:"context"`
	online.PlaylistsRequest
}
