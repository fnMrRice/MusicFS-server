// Package albums keeps explicit source-album and release identities independent
// of Recording links. Names are display metadata, never identity or merge keys.
package albums

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

const MaxAlbumSources = 1000
const MaxReleaseAlbums = 256
const MaxPageSize = 100

type SourceAlbum struct {
	ID                 string    `json:"sourceAlbumId"`
	Owner              string    `json:"-"`
	ProviderInstanceID string    `json:"providerInstanceId"`
	Name               string    `json:"name"`
	Revision           int64     `json:"revision"`
	SourceTrackIDs     []string  `json:"sourceTrackIds"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}
type Release struct {
	ID             string    `json:"releaseId"`
	Owner          string    `json:"-"`
	Name           string    `json:"name"`
	Revision       int64     `json:"revision"`
	SourceAlbumIDs []string  `json:"sourceAlbumIds"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}
type CreateSourceAlbumRequest struct {
	ProviderInstanceID string   `json:"providerInstanceId"`
	Name               string   `json:"name"`
	SourceTrackIDs     []string `json:"sourceTrackIds"`
}
type UpdateSourceAlbumRequest struct {
	ExpectedRevision int64    `json:"expectedRevision"`
	Name             string   `json:"name"`
	SourceTrackIDs   []string `json:"sourceTrackIds"`
}
type CreateReleaseRequest struct {
	Name           string   `json:"name"`
	SourceAlbumIDs []string `json:"sourceAlbumIds"`
}
type UpdateReleaseRequest struct {
	ExpectedRevision int64    `json:"expectedRevision"`
	Name             string   `json:"name"`
	SourceAlbumIDs   []string `json:"sourceAlbumIds"`
}
type Target struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type CoverBinding struct {
	ID             string    `json:"bindingId"`
	ObjectID       string    `json:"objectId"`
	TargetRevision int64     `json:"targetRevision"`
	SourceTrackID  string    `json:"sourceTrackId"`
	AssetID        string    `json:"assetId"`
	AssetVersion   string    `json:"assetVersion"`
	State          string    `json:"state"`
	CreatedAt      time.Time `json:"createdAt"`
}
type CoverSelection struct {
	Target       Target        `json:"target"`
	Revision     int64         `json:"revision"`
	ManualLocked bool          `json:"manualLocked"`
	Binding      *CoverBinding `json:"binding"`
}
type SelectCoverRequest struct {
	Target                 Target `json:"target"`
	ExpectedRevision       int64  `json:"expectedRevision"`
	ExpectedTargetRevision int64  `json:"expectedTargetRevision"`
	ObjectID               string `json:"objectId"`
	Automatic              bool   `json:"-"`
}
type UnlockCoverRequest struct {
	Target           Target `json:"target"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

// Expansion is a bounded read snapshot. Enqueuers must recheck its album
// revision and asset bindings inside their own queue transaction.
type Expansion struct {
	Album   SourceAlbum                `json:"album"`
	Targets []mediastore.ContentTarget `json:"targets"`
}
type Change struct {
	Sequence          int64     `json:"sequence"`
	Target            Target    `json:"target"`
	Area              string    `json:"area"`
	Revision          int64     `json:"revision"`
	Operation         string    `json:"operation"`
	PreviousMembers   []string  `json:"previousMembers"`
	Members           []string  `json:"members"`
	PreviousBindingID *string   `json:"previousBindingId"`
	BindingID         *string   `json:"bindingId"`
	ManualLocked      *bool     `json:"manualLocked"`
	CreatedAt         time.Time `json:"createdAt"`
}

type Repository interface {
	CreateSourceAlbum(context.Context, string, CreateSourceAlbumRequest, string) (SourceAlbum, error)
	UpdateSourceAlbum(context.Context, string, string, UpdateSourceAlbumRequest, string) (SourceAlbum, error)
	SourceAlbum(context.Context, string, string) (SourceAlbum, error)
	SourceAlbums(context.Context, string, string, string, int) ([]SourceAlbum, error)
	CreateRelease(context.Context, string, CreateReleaseRequest, string) (Release, error)
	UpdateRelease(context.Context, string, string, UpdateReleaseRequest, string) (Release, error)
	Release(context.Context, string, string) (Release, error)
	Releases(context.Context, string, string, int) ([]Release, error)
	Cover(context.Context, string, Target) (CoverSelection, error)
	SelectCover(context.Context, string, SelectCoverRequest, string, mediastore.Guard) (CoverSelection, error)
	UnlockCover(context.Context, string, UnlockCoverRequest, string) (CoverSelection, error)
	AlbumChanges(context.Context, string, Target, int64, int) ([]Change, error)
	ExpandSourceAlbum(context.Context, string, string, int64) (Expansion, error)
}
