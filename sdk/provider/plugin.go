package provider

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

// Client is the online provider boundary. Implementations own their account
// state and network transports; the worker owns framing, scopes and deadlines.
type Client interface {
	Close()
	Account(context.Context, online.AccountRequest) (online.AccountResult, error)
	Search(context.Context, online.SearchRequest) (online.SearchPage, error)
	Metadata(context.Context, string) (online.Track, error)
	TrackDetails(context.Context, string) (pluginproto.TrackDetails, error)
	ObservePlaylist(context.Context, string) (online.PlaylistObservation, error)
	Resolve(context.Context, string) error
	Album(context.Context, string) (online.Album, error)
	Artist(context.Context, string) (online.Artist, error)
	AlbumTracks(context.Context, online.EntityPageRequest) (online.SearchPage, error)
	ArtistAlbums(context.Context, online.EntityPageRequest) (online.SearchPage, error)
	ArtistTracks(context.Context, online.EntityPageRequest) (online.SearchPage, error)
	MetadataBatch(context.Context, []string) (online.TrackBatch, error)
	PlaylistTracks(context.Context, online.PlaylistTracksRequest) (online.PlaylistTracksPage, error)
	Playlists(context.Context, online.PlaylistsRequest) (online.PlaylistsPage, error)
	PublicRecommendations(context.Context, int) (online.RecommendationPage, error)
	DailySongs(context.Context, int) (online.DailySongsPage, error)
}

// Factory creates an account-isolated client after a validated host handshake.
// Transport dependencies can be captured by the factory, never supplied by RPC.
type Factory func(scope online.Scope, timeout time.Duration) (Client, error)

// PlaylistTrackWriter 由声明支持向账号歌单添加歌曲的插件实现。
type PlaylistTrackWriter interface {
	AddPlaylistTrack(context.Context, online.PlaylistTrackAddition) error
}

// Plugin supplies the independently released implementation and declarations.
type Plugin struct {
	Descriptor pluginproto.OnlineDescriptor
	Version    string
	NewClient  Factory
	// Environment 仅由受信任装配使用，不通过描述符或客户端请求授权。
	Environment []string
}

func (p Plugin) Validate() error {
	if p.NewClient == nil || !pluginproto.ValidPluginVersion(p.Version) {
		return pluginproto.Failure("PROTOCOL_MISMATCH", "Online plugin factory and semantic version are required")
	}
	return p.Descriptor.Validate(p.Descriptor.Platform)
}

func (p Plugin) supports(name string) bool {
	for _, capability := range p.Descriptor.Capabilities {
		if capability.Name == name {
			return capability.Supported
		}
	}
	return false
}

func (p Plugin) capabilities() []string {
	result := []string{}
	for _, capability := range p.Descriptor.Capabilities {
		if capability.Supported {
			result = append(result, capability.Name)
		}
	}
	return result
}
