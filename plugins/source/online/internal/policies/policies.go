// Package policies 保存官方插件拥有的声明；公共 SDK 不知道具体平台格式。
package policies

import "github.com/fnMrRice/MusicFS-server/sdk/online"

func Resources(platform string) online.ResourcePolicy {
	switch platform {
	case online.Netease:
		return Netease()
	case online.QQMusic:
		return QQMusic()
	case online.Bilibili:
		return Bilibili()
	default:
		return online.ResourcePolicy{}
	}
}

func Capabilities(platform string) []online.Capability {
	switch platform {
	case online.Netease:
		return online.DeclareCapabilities("search.tracks", "search.albums", "search.artists", "search.playlists", "metadata.album", "metadata.artist", "album.tracks", "artist.albums", "artist.tracks", "metadata.tracks.batch", "playlist.tracks", "metadata.track", "track.details", "playlist.observe", "playlist.list", "recommendations.playlists")
	case online.Bilibili:
		return online.DeclareCapabilities("search.tracks", "search.playlists", "metadata.track", "track.details", "metadata.tracks.batch", "playlist.tracks", "playlist.observe", "stream.resolve")
	default:
		return online.DeclareCapabilities()
	}
}
