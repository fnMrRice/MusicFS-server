// Package personal owns account-scoped profile, favorites, and listening history.
package personal

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
)

const (
	Favorites        = "favorites"
	Recent           = "recently-played"
	MaxFavorites     = 20000
	MaxRecent        = 1000
	HistoryRetention = 90 * 24 * time.Hour
)

type Profile struct {
	User                  auth.User `json:"user"`
	Nickname              *string   `json:"nickname"`
	Avatar                *string   `json:"avatar"`
	AvatarUploadSupported bool      `json:"avatarUploadSupported"`
}

type Entry struct {
	ForcedSourceID *string        `json:"forcedSourceTrackId"`
	MusicTrack     music.Track    `json:"musicTrack"`
	Track          *library.Track `json:"track"`
	RecordedAt     time.Time      `json:"recordedAt"`
	HistoryTarget  *HistoryTarget `json:"historyTarget,omitempty"`
}

type Page struct {
	Items      []Entry `json:"items"`
	NextCursor *string `json:"nextCursor"`
	Revision   string  `json:"revision"`
}

type PlaybackStarted struct {
	SourceTrackID     string   `json:"sourceTrackId"`
	PlaybackSessionID string   `json:"playbackSessionId"`
	AttemptID         string   `json:"attemptId"`
	SeparateTypes     []string `json:"separateTypes,omitempty"`
}

type Repository interface {
	PendingContentHistory(context.Context, string, string) ([]HistoryDelivery, error)
	ConfirmContentHistory(context.Context, string, string, string) error
	PersonalProfile(context.Context, string) (Profile, error)
	SavePersonalProfile(context.Context, string, *string) (Profile, error)
	PersonalCollection(context.Context, string, string, string, int) (Page, error)
	PutFavorite(context.Context, string, string) (Entry, error)
	SetFavoriteSource(context.Context, string, string, *string) (Entry, error)
	DeletePersonalCollection(context.Context, string, string, string) error
	RecordPlaybackStarted(context.Context, auth.Session, PlaybackStarted) (Entry, error)
}

type Service struct {
	Repository
	histories     map[string]contenttype.History
	separateTypes []string
}

func New(repo Repository) *Service { return &Service{Repository: repo} }

var safeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func ValidID(id string) bool     { return safeID.MatchString(id) }
func ValidKind(kind string) bool { return kind == Favorites || kind == Recent }
func Invalid() error             { return domain.Fail("INVALID_ARGUMENT", "Invalid personal library request") }

func ValidateNickname(nickname *string) (*string, error) {
	if nickname == nil {
		return nil, nil
	}
	value := strings.TrimSpace(*nickname)
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > 80 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return nil, Invalid()
	}
	if value == "" {
		return nil, nil
	}
	return &value, nil
}
