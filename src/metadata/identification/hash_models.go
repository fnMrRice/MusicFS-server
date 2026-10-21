package identification

import (
	"context"
	"io"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type HashTarget struct {
	SourceTrackID string `json:"sourceTrackId"`
	AssetID       string `json:"assetId"`
	AssetVersion  string `json:"assetVersion"`
	FileSizeBytes int64  `json:"-"`
}

func (t HashTarget) Binding() Binding {
	size := t.FileSizeBytes
	return Binding{SourceTrackID: t.SourceTrackID, AssetID: t.AssetID, AssetVersion: t.AssetVersion,
		Spec: Spec{Kind: "fileSha512", Origin: "probe", Algorithm: "sha512", AlgorithmVersion: "sha512-v1", ConfigRevision: "stable-full-file-v1", Range: Range{Unit: "bytes", End: &size}}}
}

// StableInput must read one immutable, complete original representation. Its
// protection remains held until Close, including throughout result publication.
type StableInput interface {
	io.ReaderAt
	Validate() error
	Publish(func() error) error
	Close() error
}

type HashScope struct{ Owner, ProviderInstanceID string }

type HashSource interface {
	ResolveHashTarget(context.Context, string, string) (HashTarget, error)
	OpenStableHash(context.Context, string, string, HashTarget) (StableInput, error)
	AutoHashScopes() []HashScope
}

type HashJob struct {
	ID string `json:"jobId"`
	HashTarget
	Owner      string        `json:"-"`
	Ticket     Ticket        `json:"-"`
	Generation int64         `json:"-"`
	Automatic  bool          `json:"-"`
	Status     string        `json:"status"`
	Progress   HashProgress  `json:"progress"`
	Error      *domain.Error `json:"error"`
	CreatedAt  time.Time     `json:"createdAt"`
	UpdatedAt  time.Time     `json:"updatedAt"`
}

type HashProgress struct {
	CompletedBytes int64 `json:"completedBytes"`
	TotalBytes     int64 `json:"totalBytes"`
}

type HashConflict struct {
	ID                 string     `json:"conflictId"`
	ProviderEvidenceID string     `json:"providerEvidenceId"`
	VerifiedEvidenceID string     `json:"verifiedEvidenceId"`
	Reason             string     `json:"reason"`
	DetectedAt         time.Time  `json:"detectedAt"`
	ResolvedAt         *time.Time `json:"resolvedAt"`
}

// HashRepository atomically persists the job and its evidence ticket. Finishing
// or cancelling changes both in one transaction; old execution generations fail.
type HashRepository interface {
	QueueHash(context.Context, string, HashTarget, string, bool) (HashJob, bool, error)
	ClaimHash(context.Context) (HashJob, error)
	GetHashJob(context.Context, string, string) (HashJob, error)
	FinishHash(context.Context, HashJob, Result, int64) error
	CancelHash(context.Context, string, string) (HashJob, error)
	HashProgress(context.Context, HashJob, int64) error
	RecoverHashes(context.Context) error
	HashCandidates(context.Context, HashScope, string, int) ([]HashTarget, error)
}
