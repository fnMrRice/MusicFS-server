// Package online defines the SDK contracts for authenticated metadata providers.
package online

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
)

const (
	Netease            = "netease"
	QQMusic            = "qqmusic"
	AppleMusic         = "applemusic"
	Bilibili           = "bilibili"
	Unversioned        = "unversioned"
	MaxPageItems       = 50
	MaxPlaylistEntries = 10000
)

// Scope is supplied by the host, never by an upstream response. Account selection
// is managed through login; callers cannot inject another AccountRef.
type Scope struct {
	UserID     string
	InstanceID string
	AccountRef string
}

type Capability struct {
	Name      string `json:"name"`
	Supported bool   `json:"supported"`
	Reason    string `json:"reason"`
}

type Track struct {
	ExternalID string   `json:"externalId"`
	Title      string   `json:"title"`
	Artists    []string `json:"artists"`
	Album      string   `json:"album"`
	DurationMs *int64   `json:"durationMs"`
	SourceURL  string   `json:"sourceUrl"`
	// MetadataDigest is not an audio asset version or proof of byte identity.
	MetadataDigest       string `json:"metadataDigest"`
	PlaybackAvailability string `json:"playbackAvailability"`
}

type SearchRequest struct {
	Kind   string
	Query  string
	Limit  int
	Cursor string
}

type SearchPage struct {
	Kind        string     `json:"kind"`
	Tracks      []Track    `json:"tracks"`
	Albums      []Album    `json:"albums"`
	Artists     []Artist   `json:"artists"`
	Playlists   []Playlist `json:"playlists"`
	NextCursor  *string    `json:"nextCursor"`
	Truncated   bool       `json:"truncated"`
	Consistency string     `json:"consistency"`
	ObservedAt  time.Time  `json:"observedAt"`
}

type Playlist struct {
	ExternalID string  `json:"externalId"`
	Title      string  `json:"title"`
	SourceURL  string  `json:"sourceUrl"`
	TrackCount *int64  `json:"trackCount"`
	Creator    *string `json:"creator"`
	CoverURL   *string `json:"coverUrl"`
}

type Recommendation struct {
	Playlist Playlist `json:"playlist"`
	Position int      `json:"position"`
	Reason   string   `json:"reason"`
}

type RecommendationPage struct {
	Items        []Recommendation `json:"items"`
	Personalized bool             `json:"personalized"`
	NextCursor   *string          `json:"nextCursor"`
	ObservedAt   time.Time        `json:"observedAt"`
	ExpiresAt    time.Time        `json:"expiresAt"`
}

// Position identifies an occurrence only within this observation. It is not a
// stable remote entry ID; repeated song IDs are intentionally retained.
type PlaylistEntry struct {
	Position        int    `json:"position"`
	ExternalTrackID string `json:"externalTrackId"`
}

type PlaylistObservation struct {
	Playlist       Playlist        `json:"playlist"`
	Entries        []PlaylistEntry `json:"entries"`
	Complete       bool            `json:"complete"`
	Consistency    string          `json:"consistency"`
	MetadataDigest string          `json:"metadataDigest"`
	ObservedAt     time.Time       `json:"observedAt"`
}

// Capabilities defines the host's verified upper bound for official platforms.
// A particular plugin's actual enabled capabilities come from its descriptor.
func Capabilities(platform string) []Capability {
	if platform == Bilibili {
		return bilibiliCapabilities()
	}
	return []Capability{
		{"search.tracks", platform == Netease, "Login required; Netease EAPI and QQ desktop RPC; real account queries pending; Apple catalog unsupported"},
		{"search.albums", platform == Netease, "Netease authenticated client public search verified"},
		{"search.artists", platform == Netease, "Netease authenticated client public search verified"},
		{"search.playlists", platform == Netease, "Netease authenticated client public search verified"},
		{"metadata.album", platform == Netease, "Netease authenticated client album detail verified"},
		{"metadata.artist", platform == Netease, "Netease authenticated client artist detail verified"},
		{"album.tracks", platform == Netease, "Digest-bound album observation with paged tracks"},
		{"artist.albums", platform == Netease, "Netease public artist album paging verified"},
		{"artist.tracks", platform == Netease, "Netease public artist track paging verified; order remains upstream hot order"},
		{"metadata.tracks.batch", platform == Netease, "At most 50 public track identities per request"},
		{"playlist.tracks", platform == Netease, "Digest-bound observation with batched display metadata; no audio authorization"},
		{"metadata.track", platform == Netease, "Only Netease authenticated client detail has been verified"},
		{"track.details", platform == Netease, "Provider-owned read-only track detail fields"},
		{"playlist.observe", platform == Netease, "Unversioned public entry observations only"},
		{"playlist.list", platform == Netease, "Current account playlists; account-bound paging tested; live account validation pending"},
		{"recommendations.playlists", platform == Netease, "Login required; first-page recommendations; personalization is not verified"},
		{"account.login", false, "Login adapters are available for evaluation; live account validation is pending"},
		{"stream.resolve", false, "No authorized complete audio resource has been verified"},
	}
}

// TextValue validates UTF-8 text against a byte limit and strips outer space.
func TextValue(s string, limit int) (string, error) {
	if !utf8.ValidString(s) || len(s) > limit || strings.ContainsFunc(s, unicode.IsControl) {
		return "", UpstreamInvalid()
	}
	return strings.TrimSpace(s), nil
}

// Digest computes the observation digest of a JSON-compatible DTO value.
// It does not represent an audio resource version or authorization.
func Digest(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// UpstreamInvalid creates the shared error for malformed platform metadata.
func UpstreamInvalid() error {
	return fault.Fail("TEMPORARY_FAILURE", "Upstream metadata response is invalid")
}
