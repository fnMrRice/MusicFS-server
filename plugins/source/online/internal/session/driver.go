package session

import (
	"context"
	"net/http"
	"net/url"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

type HTTP func(context.Context, string, string, string, map[string]string, map[string]string) ([]byte, *http.Response, error)

// Runtime exposes shared account, transport and paging services to one driver.
// Its owner holds the account lock while invoking the driver.
type Runtime struct {
	Pending            *Secret
	Secret             *Secret
	View               *online.AccountView
	Key                *string
	HTTP               HTTP
	Complete           func()
	Admit              func(context.Context) error
	BusinessError      func(int, int) error
	Get                func(context.Context, string, url.Values, any) error
	Require            func(context.Context) error
	ReadCursor         func(online.SearchRequest) (int, error)
	ReadCursorToken    func(online.SearchRequest) (Cursor, error)
	WriteCursor        func(online.SearchRequest, int) string
	WriteVersionCursor func(online.SearchRequest, int, string) string
	WaitForMetadata    func(context.Context) error
}

type Driver interface {
	Begin(context.Context, online.AccountRequest) error
	Poll(context.Context, online.AccountRequest) error
	Check(context.Context, *Secret) error
	AvatarURL(string) string
	Request(context.Context, *Secret, string, map[string]any, any) error
	Search(context.Context, online.SearchRequest, int) ([]online.Track, bool, error)
	SearchEntities(context.Context, online.SearchRequest) (online.SearchPage, error)
	Metadata(context.Context, string) (online.Track, error)
	ObservePlaylist(context.Context, string) (online.PlaylistObservation, error)
	Album(context.Context, string) (online.Album, error)
	Artist(context.Context, string) (online.Artist, error)
	AlbumTracks(context.Context, online.EntityPageRequest) (online.SearchPage, error)
	ArtistAlbums(context.Context, online.EntityPageRequest) (online.SearchPage, error)
	ArtistTracks(context.Context, online.EntityPageRequest) (online.SearchPage, error)
	MetadataBatch(context.Context, []string) (online.TrackBatch, error)
	PlaylistTracks(context.Context, online.PlaylistTracksRequest) (online.PlaylistTracksPage, error)
	Playlists(context.Context, online.PlaylistsRequest) (online.PlaylistsPage, error)
	PublicRecommendations(context.Context, int) (online.RecommendationPage, error)
}

type Factory func(Runtime) Driver
