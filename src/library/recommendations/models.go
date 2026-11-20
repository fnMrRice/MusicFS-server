// Package recommendations reads scoped public recommendations without importing
// playlists, creating library tracks, or resolving playback resources.
package recommendations

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

const MaxSources = 8
const MaxHistory = 5

type Provider interface {
	PublicRecommendations(context.Context, int) (online.RecommendationPage, error)
}

// Runtime is trusted composition data. Name is the plugin-owned alias; Provider
// must already be bound to Owner and InstanceID. Composition leaves AccountID
// empty; AccountScope supplies the validated login generation for cache access.
type Runtime struct {
	Owner, InstanceID, Name, Platform, AccountID, ConfigRevision string
	Provider                                                     Provider
	// InstanceName resolves the current account display name or plugin alias.
	// It runs outside the service lock for every response, including cache hits.
	InstanceName func(context.Context) string
	Gate         *plugincontrol.Gate
	generation   uint64
	epoch        uint64
}

type Config struct {
	Repository      Repository
	Now             func() time.Time
	MaxEntries      int
	MaxBytes        int64
	MaxConcurrent   int
	TTL             time.Duration
	Timeout         time.Duration
	FailureCooldown time.Duration
}

type Request struct {
	InstanceIDs []string `json:"instanceIds"`
	Limit       int      `json:"limit"`
	Refresh     bool     `json:"refresh"`
	History     int      `json:"history"`
	day         string
}

type Failure struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	RetryAfter *int   `json:"retryAfter"`
}

type Source struct {
	InstanceID string `json:"instanceId"`
	Name       string `json:"name"`
	Platform   string `json:"platform"`
	// Together with Items[].Playlist.ExternalID these fields construct the
	// explicit P7 playlist import RemoteRef; no import happens during List.
	AccountID     string                  `json:"accountId"`
	Items         []online.Recommendation `json:"items"`
	Personalized  bool                    `json:"personalized"`
	NextCursor    *string                 `json:"nextCursor"`
	Stale         bool                    `json:"stale"`
	LastSuccessAt *time.Time              `json:"lastSuccessAt"`
	ExpiresAt     *time.Time              `json:"expiresAt"`
	Error         *Failure                `json:"error"`
}

type Response struct {
	Sources      []Source `json:"sources"`
	HistoryDepth int      `json:"historyDepth"`
}

// SavedPage 保存服务器日历日期以及当时整页各来源的结果。
type SavedPage struct {
	Day          string   `json:"day"`
	Sources      []Source `json:"sources"`
	HistoryDepth int      `json:"historyDepth"`
}

type Repository interface {
	ReadRecommendationPage(ctx context.Context, owner, scope string, history int) (SavedPage, error)
	SaveRecommendationPage(ctx context.Context, owner, scope, day string, sources []Source, successful bool) (int, error)
}

type cacheKey struct {
	owner, instance, account, revision string
	limit                              int
}
type runtimeKey struct{ owner, instance string }
type entry struct {
	source         Source
	day            string
	bytes          int64
	used           uint64
	expires, retry time.Time
	flight         *flight
}
type flight struct {
	done      chan struct{}
	day       string
	cancel    context.CancelFunc
	waiters   int
	finished  bool
	abandoned bool
	result    Source
	lease     *plugincontrol.Lease
}

func sourceFor(r Runtime) Source {
	return Source{InstanceID: r.InstanceID, Name: r.Name, Platform: r.Platform, AccountID: r.AccountID, Items: []online.Recommendation{}}
}

func cloneSource(s Source) Source {
	items := make([]online.Recommendation, len(s.Items))
	copy(items, s.Items)
	s.Items = items
	for i := range s.Items {
		if p := s.Items[i].Playlist.Creator; p != nil {
			v := *p
			s.Items[i].Playlist.Creator = &v
		}
		if p := s.Items[i].Playlist.CoverURL; p != nil {
			v := *p
			s.Items[i].Playlist.CoverURL = &v
		}
		if p := s.Items[i].Playlist.TrackCount; p != nil {
			v := *p
			s.Items[i].Playlist.TrackCount = &v
		}
	}
	if s.LastSuccessAt != nil {
		v := *s.LastSuccessAt
		s.LastSuccessAt = &v
	}
	if s.ExpiresAt != nil {
		v := *s.ExpiresAt
		s.ExpiresAt = &v
	}
	if s.Error != nil {
		f := *s.Error
		s.Error = &f
		if f.RetryAfter != nil {
			n := *f.RetryAfter
			s.Error.RetryAfter = &n
		}
	}
	return s
}
