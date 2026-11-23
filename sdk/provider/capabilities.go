package provider

func methodCapability(method string) string {
	switch method {
	case "provider.online.metadata":
		return "metadata.track"
	case "provider.online.trackDetails":
		return "track.details"
	case "provider.online.playlist":
		return "playlist.observe"
	case "provider.online.resolve", "provider.online.audioDescribe", "provider.online.audioOpen", "provider.online.audioRead", "provider.online.audioClose":
		return "stream.resolve"
	case "provider.online.album":
		return "metadata.album"
	case "provider.online.artist":
		return "metadata.artist"
	case "provider.online.albumTracks":
		return "album.tracks"
	case "provider.online.artistAlbums":
		return "artist.albums"
	case "provider.online.artistTracks":
		return "artist.tracks"
	case "provider.online.metadataBatch":
		return "metadata.tracks.batch"
	case "provider.online.playlistTracks":
		return "playlist.tracks"
	case "provider.online.playlists":
		return "playlist.list"
	case "provider.online.addPlaylistTrack":
		return "playlist.addTracks"
	case "provider.online.recommendations":
		return "recommendations.playlists"
	case "provider.online.dailySongs":
		return "recommendations.tracks"
	default:
		// Search selects its capability after decoding the search kind.
		// Account and descriptor methods are always available control messages.
		return ""
	}
}
