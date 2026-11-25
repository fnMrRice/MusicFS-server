// Package music 定义曲目资料及其与歌手、专辑和插件来源的关系。
package music

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
)

type Artist struct {
	ID             string                   `json:"artistId"`
	Name           string                   `json:"name"`
	Revision       int64                    `json:"namesRevision,omitempty"`
	LocalizedNames []scraper.LocalizedTitle `json:"localizedNames,omitempty"`
}

type ArtistNamesInput struct {
	LocalizedNames []scraper.LocalizedTitle `json:"localizedNames"`
}

type AlbumFields struct {
	Name        string            `json:"name"`
	ExternalID  *string           `json:"externalId"`
	IsExternal  bool              `json:"isExternal"`
	CoverPath   *string           `json:"coverPath"`
	Publisher   *string           `json:"publisher"`
	ReleaseDate *string           `json:"releaseDate"`
	SourceLinks []AlbumSourceLink `json:"sourceLinks"`
}

type AlbumSourceLink struct {
	PluginID string `json:"pluginId"`
	URL      string `json:"url"`
}

type Album struct {
	ID string `json:"albumId"`
	AlbumFields
	OriginalName string `json:"originalName"`
	Revision     int64  `json:"revision"`
}

type Source struct {
	Downloaded       bool                  `json:"downloaded"`
	DownloadVersions []onlinecache.Version `json:"downloadVersions,omitempty"`
	SourceTrackID    string                `json:"sourceTrackId"`
	PluginID         *string               `json:"pluginId"`
	InstanceID       string                `json:"providerInstanceId"`
	SourceID         string                `json:"sourceId"`
	CreatedAt        string                `json:"createdAt"`
	UpdatedAt        string                `json:"updatedAt"`
}

type TrackFields struct {
	Title           string                   `json:"title"`
	LocalizedTitles []scraper.LocalizedTitle `json:"localizedTitles,omitzero"`
	CoverPath       *string                  `json:"coverPath"`
	Genre           *string                  `json:"genre"`
	Year            *int                     `json:"year"`
	Rating          *int                     `json:"rating"`
}

type Track struct {
	ID string `json:"trackId"`
	TrackFields
	Revision       int64          `json:"revision"`
	Artists        []Artist       `json:"artists"`
	Albums         []Album        `json:"albums"`
	AlbumIDs       []string       `json:"albumIds"`
	Sources        []Source       `json:"sources"`
	SelectedSource *library.Track `json:"selectedSource"`
	CreatedAt      string         `json:"createdAt"`
	UpdatedAt      string         `json:"updatedAt"`
}

type TrackInput struct {
	TrackFields
	ArtistNames []string `json:"artistNames"`
	AlbumIDs    []string `json:"albumIds"`
}

type Query struct {
	After, Q, ProviderID, ArtistID, AlbumID, Genre, Unknown string
	Limit                                                   int
	SourceOnly                                              bool
	UnscrapedOnly                                           bool
}

type ArtistItem struct {
	Artist
	TrackCount int `json:"trackCount"`
}

type AlbumItem struct {
	Album
	TrackCount int `json:"trackCount"`
}

// AlbumScrapeTarget 从可见的专辑成员取得真实音乐源和最常见的艺人署名。
type AlbumScrapeTarget struct {
	Name, Artist, SourcePluginID string
	Release                      *audiotypes.AlbumRelease `json:"release,omitempty"`
}

type ArtistPage struct {
	Items     []ArtistItem `json:"items"`
	NextAfter *string      `json:"nextAfter"`
}

type GenreItem struct {
	Name       string `json:"name"`
	TrackCount int    `json:"trackCount"`
}

type GenrePage struct {
	Items     []GenreItem `json:"items"`
	NextAfter *string     `json:"nextAfter"`
}

type AlbumPage struct {
	Items     []AlbumItem `json:"items"`
	NextAfter *string     `json:"nextAfter"`
}

type TrackPage struct {
	Items            []Track `json:"items"`
	NextAfter        *string `json:"nextAfter"`
	LibraryRevision  string  `json:"libraryRevision"`
	MatchingRevision int64   `json:"matchingRevision"`
}

type Repository interface {
	MusicTrack(context.Context, string, string) (Track, error)
	MusicTrackForSource(context.Context, string, string) (Track, error)
	MusicTracks(context.Context, string, Query) (TrackPage, error)
	SaveMusicTrack(context.Context, string, string, int64, TrackInput, string) (Track, error)
	MergeMusicAlbumTracks(context.Context, string, string, MergeTracksInput, string) (Track, error)
	MusicArtists(context.Context, string, Query) (ArtistPage, error)
	MusicArtist(context.Context, string, string) (Artist, error)
	SaveMusicArtistNames(context.Context, string, string, int64, ArtistNamesInput, string) (Artist, error)
	MusicGenres(context.Context, string, Query) (GenrePage, error)
	MusicAlbum(context.Context, string, string) (Album, error)
	MusicAlbumScrapeTarget(context.Context, string, string, string) (AlbumScrapeTarget, error)
	MusicAlbumTrackEvidence(context.Context, string, string, string) ([]AlbumTrackEvidence, error)
	MusicAlbums(context.Context, string, Query) (AlbumPage, error)
	SaveMusicAlbum(context.Context, string, string, int64, AlbumFields, string) (Album, error)
	MusicAlbumScrapedFields(context.Context, string, string) ([]works.ScrapedField, error)
	ClearMusicAlbumScrapedFields(context.Context, string, string, int64, string) (int64, error)
	SelectMusicAlbumScrapedField(context.Context, string, string, int64, works.ScrapedField, string) (int64, error)
	SelectMusicAlbumScrapedFields(context.Context, string, string, int64, []works.ScrapedField, string) (int64, error)
	SelectAutoMusicAlbumScrapedFields(context.Context, string, string, string, int64, []works.ScrapedField, string) (int64, error)
	MergeMusicAlbumMetadata(context.Context, string, string, int64, []works.ScrapedField, []string, string) (int64, error)
}
