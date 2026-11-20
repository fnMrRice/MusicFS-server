// Package playlists preserves remote observations and explicit personal imports.
package playlists

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

const MaxEntries = online.MaxPlaylistEntries
const MaxSnapshotEntries = 2 * MaxEntries
const MaxPageSize = 100

// AccountID is empty for the explicitly anonymous host scope. It is not a token.
type RemoteRef struct {
	InstanceID string `json:"instanceId"`
	AccountID  string `json:"accountId"`
	ExternalID string `json:"externalId"`
}

// Binding is host-owned configuration captured in a persistent execution ticket.
type Binding struct {
	RemoteRef
	Platform       string `json:"platform"`
	ConfigRevision string `json:"configRevision"`
}

type RemotePlaylist struct {
	ID    string `json:"playlistId"`
	Owner string `json:"-"`
	RemoteRef
	Platform        string        `json:"platform"`
	ConfigRevision  string        `json:"-"`
	BindingRevision int64         `json:"bindingRevision"`
	Revision        int64         `json:"revision"`
	Title           string        `json:"title"`
	Stale           bool          `json:"stale"`
	LastSuccessAt   *time.Time    `json:"lastSuccessAt"`
	Error           *domain.Error `json:"error"`
	CreatedAt       time.Time     `json:"createdAt"`
	UpdatedAt       time.Time     `json:"updatedAt"`
}

// RemoteSource never invents an audio asset for metadata-only platform entries.
type RemoteSource struct {
	ID string `json:"remoteSourceId"`
	RemoteRef
	PlaybackAvailability string `json:"playbackAvailability"`
}

// Occurrence identity is retained by external track ID and occurrence order.
// Platforms without stable entry IDs cannot distinguish identical duplicates.
type Entry struct {
	ID                      string       `json:"entryId"`
	Position                int          `json:"position"`
	Source                  RemoteSource `json:"source"`
	State                   string       `json:"state"`
	LastObservedPosition    int          `json:"lastObservedPosition"`
	FirstMissingObservation *string      `json:"firstMissingObservation"`
}

type Removal struct {
	EntryID                 string `json:"entryId"`
	RemoteSourceID          string `json:"remoteSourceId"`
	FirstMissingObservation string `json:"firstMissingObservation"`
	ConfirmedObservation    string `json:"confirmedObservation"`
}

type Snapshot struct {
	PlaylistID      string          `json:"playlistId"`
	Revision        int64           `json:"revision"`
	BindingRevision int64           `json:"bindingRevision"`
	ObservationID   string          `json:"observationId"`
	Consistency     string          `json:"consistency"`
	RemoteVersion   *string         `json:"remoteVersion"`
	MetadataDigest  string          `json:"metadataDigest"`
	Metadata        online.Playlist `json:"metadata"`
	ObservedAt      time.Time       `json:"observedAt"`
	CreatedAt       time.Time       `json:"createdAt"`
	Entries         []Entry         `json:"entries"`
	Removals        []Removal       `json:"removals"`
}

type SnapshotPage struct {
	PlaylistID      string    `json:"playlistId"`
	Revision        int64     `json:"revision"`
	BindingRevision int64     `json:"bindingRevision"`
	ObservationID   string    `json:"observationId"`
	Consistency     string    `json:"consistency"`
	RemoteVersion   *string   `json:"remoteVersion"`
	ObservedAt      time.Time `json:"observedAt"`
	Total           int       `json:"total"`
	Items           []Entry   `json:"items"`
	NextOffset      *int      `json:"nextOffset"`
}

type SyncJob struct {
	ID              string        `json:"jobId"`
	Owner           string        `json:"-"`
	PlaylistID      string        `json:"playlistId"`
	Binding         Binding       `json:"-"`
	BindingRevision int64         `json:"bindingRevision"`
	Generation      int64         `json:"generation"`
	State           string        `json:"state"`
	ResultRevision  *int64        `json:"resultRevision"`
	Error           *domain.Error `json:"error"`
	CreatedAt       time.Time     `json:"createdAt"`
	UpdatedAt       time.Time     `json:"updatedAt"`
}

// Version is only for an adapter with a verified, fixed remote snapshot token.
// Current public Netease observations always have nil Version/unversioned.
type Observation struct {
	online.PlaylistObservation
	Version *string
}

type Remote interface {
	Authorize(context.Context, string, RemoteRef) (Binding, error)
	ObservePlaylist(context.Context, string, Binding) (Observation, error)
}

type Repository interface {
	EnsurePlaylistTrackSources(context.Context, string, string, string, []string) ([]RemoteSource, error)
	ImportRemotePlaylist(context.Context, string, Binding, string) (RemotePlaylist, bool, error)
	RemotePlaylist(context.Context, string, string) (RemotePlaylist, error)
	RemotePlaylists(context.Context, string, string, int) ([]RemotePlaylist, error)
	QueueRemoteSync(context.Context, string, string, int64, Binding, string) (SyncJob, bool, error)
	RemoteSyncJob(context.Context, string, string) (SyncJob, error)
	CancelRemoteSync(context.Context, string, string) (SyncJob, error)
	ClaimRemoteSync(context.Context) (SyncJob, error)
	FinishRemoteSync(context.Context, SyncJob, Observation) (Snapshot, error)
	FailRemoteSync(context.Context, SyncJob, *domain.Error) error
	RecoverRemoteSync(context.Context) error
	RemoteSnapshot(context.Context, string, string, int64) (Snapshot, error)
	RemoteSource(context.Context, string, string) (RemoteSource, error)
}
