package driver

import (
	"github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/policies"
	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

const (
	Unversioned        = online.Unversioned
	MaxPageItems       = online.MaxPageItems
	MaxPlaylistEntries = online.MaxPlaylistEntries
)

type Album = online.Album
type Artist = online.Artist
type EntityPageRequest = online.EntityPageRequest
type TrackLookup = online.TrackLookup
type TrackBatch = online.TrackBatch
type PlaylistTracksRequest = online.PlaylistTracksRequest
type PlaylistTrack = online.PlaylistTrack
type PlaylistTracksPage = online.PlaylistTracksPage
type PlaylistsRequest = online.PlaylistsRequest
type PlaylistsPage = online.PlaylistsPage
type Track = online.Track
type SearchRequest = online.SearchRequest
type SearchPage = online.SearchPage
type Playlist = online.Playlist
type Recommendation = online.Recommendation
type RecommendationPage = online.RecommendationPage
type PlaylistEntry = online.PlaylistEntry
type PlaylistObservation = online.PlaylistObservation

func textValue(value string, limit int) (string, error) { return online.TextValue(value, limit) }
func digest(value any) string                           { return online.Digest(value) }
func upstreamInvalid() error                            { return online.UpstreamInvalid() }
func imageURL(value string) (*string, error)            { return policies.Netease().ImageURL(value) }
func validCount(value *int64) bool                      { return online.ValidCount(value) }
func playlistDigest(playlist Playlist, entries []PlaylistEntry) string {
	return online.PlaylistDigest(playlist, entries)
}
func invalidCursor() error {
	return fault.Fail("INVALID_ARGUMENT", "Online search cursor is invalid or expired")
}
func platformLoginRequired() error {
	return fault.Fail("PLATFORM_AUTH_REQUIRED", "Sign in to the music platform before using this source")
}
