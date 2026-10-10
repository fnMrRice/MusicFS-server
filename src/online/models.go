// Package online reads bounded public platform metadata without resolving audio.
package online

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

const (
	Netease            = "netease"
	QQMusic            = "qqmusic"
	AppleMusic         = "applemusic"
	Unversioned        = "unversioned"
	MaxPageItems       = 50
	MaxPlaylistEntries = 10000
)

// Scope is supplied by the host, never by an upstream response. Anonymous
// adapters reject a nonempty AccountRef rather than silently discarding it.
type Scope struct {
	UserID     string
	InstanceID string
	AccountRef string
}

// Options are trusted composition inputs, not fields accepted from a client.
// Transport is a test/host injection point; nil uses fixed-host, public-IP TLS.
type Options struct {
	Transport http.RoundTripper
	Timeout   time.Duration
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
	Query  string
	Limit  int
	Cursor string
}

type SearchPage struct {
	Tracks      []Track   `json:"tracks"`
	NextCursor  *string   `json:"nextCursor"`
	Truncated   bool      `json:"truncated"`
	Consistency string    `json:"consistency"`
	ObservedAt  time.Time `json:"observedAt"`
}

type Playlist struct {
	ExternalID string `json:"externalId"`
	Title      string `json:"title"`
	SourceURL  string `json:"sourceUrl"`
	TrackCount *int64 `json:"trackCount"`
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

func (c *Client) Scope() Scope { return c.scope }

func (c *Client) Capabilities() []Capability {
	return Capabilities(c.platform)
}

// Capabilities describes only capabilities verified for this adapter revision.
func Capabilities(platform string) []Capability {
	return []Capability{
		{"search.tracks", platform == Netease, "Netease verified; QQ adapter exists but its latest live check failed; Apple requires credentials"},
		{"metadata.track", platform == Netease, "Only Netease anonymous detail has been verified"},
		{"playlist.observe", platform == Netease, "Unversioned public entry observations only"},
		{"recommendations.playlists", platform == Netease, "Anonymous first page only; no personalized recommendations"},
		{"account.login", false, "Account authorization has not been implemented or verified"},
		{"stream.resolve", false, "No authorized complete audio resource has been verified"},
	}
}

// Resolve is deliberately unable to return a URL or headers for audio.
func (c *Client) Resolve(ctx context.Context, externalID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return domain.Fail("UNSUPPORTED", "This adapter does not resolve audio resources")
}

func textValue(s string, limit int) (string, error) {
	if !utf8.ValidString(s) || len(s) > limit || strings.ContainsFunc(s, unicode.IsControl) {
		return "", upstreamInvalid()
	}
	return strings.TrimSpace(s), nil
}

func digest(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func upstreamInvalid() error {
	return domain.Fail("TEMPORARY_FAILURE", "Upstream metadata response is invalid")
}
