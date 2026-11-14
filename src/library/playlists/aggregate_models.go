package playlists

import (
	"context"
	"time"
)

const MaxBindings = 128
const MaxGraphPlaylists = 128
const MaxGraphDepth = 16
const MaxContributions = 20000

type SourceRef struct {
	TrackID        *string `json:"trackId,omitempty"`
	ForcedSourceID *string `json:"forcedSourceTrackId,omitempty"`
	SourceTrackID  *string `json:"sourceTrackId"`
	RemoteSourceID *string `json:"remoteSourceId"`
}
type LocalEntry struct {
	ID     string    `json:"entryId"`
	Source SourceRef `json:"source"`
}
type PlaylistBinding struct {
	ID       string       `json:"bindingId"`
	Kind     string       `json:"kind"`
	TargetID *string      `json:"targetId"`
	Entries  []LocalEntry `json:"entries"`
}
type Playlist struct {
	Kind             string            `json:"kind"`
	ID               string            `json:"playlistId"`
	Name             string            `json:"name"`
	RuleRevision     int64             `json:"ruleRevision"`
	HideRevision     int64             `json:"hideRevision"`
	SnapshotRevision int64             `json:"snapshotRevision"`
	Stale            bool              `json:"stale"`
	Bindings         []PlaylistBinding `json:"bindings"`
	CreatedAt        time.Time         `json:"createdAt"`
	UpdatedAt        time.Time         `json:"updatedAt"`
}
type CreatePlaylistRequest struct {
	Name     string            `json:"name"`
	Bindings []PlaylistBinding `json:"bindings"`
}
type EditPlaylistRequest struct {
	ExpectedRevision int64             `json:"expectedRevision"`
	Name             string            `json:"name"`
	Bindings         []PlaylistBinding `json:"bindings"`
}
type RebuildRequest struct {
	ExpectedRuleRevision int64 `json:"expectedRuleRevision"`
}
type Provenance struct {
	BindingPath      []string  `json:"bindingPath"`
	OriginPlaylistID string    `json:"originPlaylistId"`
	EntryID          string    `json:"entryId"`
	Source           SourceRef `json:"source"`
	State            string    `json:"state"`
	Hidden           bool      `json:"hidden"`
}
type AggregateEntry struct {
	ID          string       `json:"entryId"`
	Position    int          `json:"position"`
	RecordingID *string      `json:"recordingId"`
	Source      SourceRef    `json:"source"`
	Hidden      bool         `json:"hidden"`
	Provenance  []Provenance `json:"provenance"`
}
type Dependency struct {
	Kind     string `json:"kind"`
	ID       string `json:"playlistId"`
	Revision int64  `json:"revision"`
}
type AggregateSnapshot struct {
	PlaylistID        string           `json:"playlistId"`
	Revision          int64            `json:"revision"`
	RuleRevision      int64            `json:"ruleRevision"`
	HideRevision      int64            `json:"hideRevision"`
	MatchingRevision  int64            `json:"matchingRevision"`
	Dependencies      []Dependency     `json:"dependencies"`
	Entries           []AggregateEntry `json:"entries"`
	NeedsConfirmation []string         `json:"needsConfirmation"`
	CreatedAt         time.Time        `json:"createdAt"`
}
type AggregatePage struct {
	PlaylistID        string           `json:"playlistId"`
	Revision          int64            `json:"revision"`
	RuleRevision      int64            `json:"ruleRevision"`
	HideRevision      int64            `json:"hideRevision"`
	MatchingRevision  int64            `json:"matchingRevision"`
	Total             int              `json:"total"`
	Items             []AggregateEntry `json:"items"`
	NextOffset        *int             `json:"nextOffset"`
	NeedsConfirmation []string         `json:"needsConfirmation"`
}

// Hiding always keeps the original source anchor. The private permitted-member
// set is captured when this rule is explicitly created or confirmed.
type HiddenRule struct {
	ID                string    `json:"ruleId"`
	Scope             string    `json:"scope"`
	Source            SourceRef `json:"source"`
	BindingPath       []string  `json:"bindingPath"`
	EntryID           *string   `json:"entryId"`
	Active            bool      `json:"active"`
	NeedsConfirmation bool      `json:"needsConfirmation"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}
type HiddenRules struct {
	PlaylistID string       `json:"playlistId"`
	Revision   int64        `json:"revision"`
	Items      []HiddenRule `json:"items"`
}
type HideRequest struct {
	ExpectedRevision int64     `json:"expectedRevision"`
	Scope            string    `json:"scope"`
	Source           SourceRef `json:"source"`
	BindingPath      []string  `json:"bindingPath"`
	EntryID          *string   `json:"entryId"`
}
type ChangeHiddenRequest struct {
	ExpectedRevision int64 `json:"expectedRevision"`
	Active           bool  `json:"active"`
	Confirm          bool  `json:"confirm"`
}
type PlaylistChange struct {
	Sequence     int64     `json:"sequence"`
	PlaylistID   string    `json:"playlistId"`
	Operation    string    `json:"operation"`
	RuleRevision int64     `json:"ruleRevision"`
	HideRevision int64     `json:"hideRevision"`
	RuleID       *string   `json:"ruleId"`
	CreatedAt    time.Time `json:"createdAt"`
}
type AggregateRepository interface {
	CreatePlaylist(context.Context, string, CreatePlaylistRequest, string) (Playlist, error)
	EditPlaylist(context.Context, string, string, EditPlaylistRequest, string) (Playlist, error)
	Playlist(context.Context, string, string) (Playlist, error)
	Playlists(context.Context, string, string, int) ([]Playlist, error)
	SearchPlaylists(context.Context, string, string, string, int) ([]Playlist, error)
	RebuildPlaylist(context.Context, string, string, RebuildRequest, string) (AggregateSnapshot, error)
	PlaylistSnapshot(context.Context, string, string, int64) (AggregateSnapshot, error)
	HiddenRules(context.Context, string, string) (HiddenRules, error)
	HidePlaylistSource(context.Context, string, string, HideRequest, string) (HiddenRules, error)
	ChangePlaylistHidden(context.Context, string, string, string, ChangeHiddenRequest, string) (HiddenRules, error)
	PlaylistChanges(context.Context, string, string, int64, int) ([]PlaylistChange, error)
}
type AggregateService struct{ AggregateRepository }

func NewAggregate(repo AggregateRepository) *AggregateService { return &AggregateService{repo} }
func (s *AggregateService) Page(ctx context.Context, owner, id string, revision int64, offset, limit int) (AggregatePage, error) {
	snapshot, err := s.PlaylistSnapshot(ctx, owner, id, revision)
	if err != nil {
		return AggregatePage{}, err
	}
	return AggregateSnapshotPage(snapshot, offset, limit)
}
