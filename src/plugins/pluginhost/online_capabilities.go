package pluginhost

import (
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// Control operations remain available even when all business capabilities are
// disabled. Business access follows the verified worker declaration.
func (w *OnlineWorker) requireOnlineCapability(method string, request any) error {
	capability := ""
	switch method {
	case "provider.online.describe", "provider.online.account", "provider.online.accountOptions":
		return nil
	case "provider.online.search":
		r, ok := request.(pluginproto.OnlineSearch)
		if !ok {
			return domain.Fail("INVALID_ARGUMENT", "Invalid online search request")
		}
		kind := r.Kind
		if kind == "" {
			kind = "tracks"
		}
		capability = "search." + kind
	case "provider.online.metadata":
		capability = "metadata.track"
	case "provider.online.trackDetails":
		capability = "track.details"
	case "provider.online.playlist":
		capability = "playlist.observe"
	case "provider.online.resolve", "provider.online.audioDescribe", "provider.online.audioOpen", "provider.online.audioRead", "provider.online.audioClose":
		capability = "stream.resolve"
	case "provider.online.album":
		capability = "metadata.album"
	case "provider.online.artist":
		capability = "metadata.artist"
	case "provider.online.albumTracks":
		capability = "album.tracks"
	case "provider.online.artistAlbums":
		capability = "artist.albums"
	case "provider.online.artistTracks":
		capability = "artist.tracks"
	case "provider.online.metadataBatch":
		capability = "metadata.tracks.batch"
	case "provider.online.playlistTracks":
		capability = "playlist.tracks"
	case "provider.online.playlists":
		capability = "playlist.list"
	case "provider.online.addPlaylistTrack":
		capability = "playlist.addTracks"
	case "provider.online.recommendations":
		capability = "recommendations.playlists"
	case "provider.online.dailySongs":
		capability = "recommendations.tracks"
	}
	for _, declaration := range w.descriptor.Capabilities {
		if declaration.Name == capability && declaration.Supported {
			return nil
		}
	}
	return domain.Fail("UNSUPPORTED", "Online plugin does not declare the requested capability")
}
