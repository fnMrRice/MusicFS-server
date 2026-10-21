package identification

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprint"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

type FingerprintRequest struct {
	StartMs           int64                `json:"startMs"`
	DurationMs        int64                `json:"durationMs"`
	ExtractorIdentity fingerprint.Identity `json:"extractorIdentity"`
}

type FingerprintTarget struct {
	HashTarget
	FingerprintRequest
}

// FingerprintSummary is the bounded publication journal payload. Raw fingerprint
// values exist only in the separately validated media file, never in this DTO.
type FingerprintSummary struct {
	ExtractorIdentity fingerprint.Identity `json:"extractorIdentity"`
	ActualStartSample int64                `json:"actualStartSample"`
	SampleCount       int64                `json:"sampleCount"`
	ValueCount        int                  `json:"valueCount"`
	Quality           fingerprint.Quality  `json:"quality"`
	ComputedAt        time.Time            `json:"computedAt"`
}

type FingerprintIndex struct {
	ID string `json:"fingerprintId"`
	FingerprintTarget
	Object            mediastore.Object   `json:"object"`
	ActualStartSample int64               `json:"actualStartSample"`
	SampleCount       int64               `json:"sampleCount"`
	ValueCount        int                 `json:"valueCount"`
	Quality           fingerprint.Quality `json:"quality"`
	ComputedAt        time.Time           `json:"computedAt"`
	Current           bool                `json:"current"`
}

type FingerprintJob struct {
	ID string `json:"jobId"`
	FingerprintTarget
	Owner      string            `json:"-"`
	Generation int64             `json:"-"`
	Automatic  bool              `json:"-"`
	ResultID   string            `json:"-"`
	Status     string            `json:"status"`
	Error      *domain.Error     `json:"error"`
	Result     *FingerprintIndex `json:"result"`
	CreatedAt  time.Time         `json:"createdAt"`
	UpdatedAt  time.Time         `json:"updatedAt"`
}

// Successful jobs are finalized only by the publication repository while the
// worker holds StableInput.Publish. Failure/cancellation cannot create indexes.
type FingerprintRepository interface {
	QueueFingerprint(context.Context, string, FingerprintTarget, string, bool) (FingerprintJob, bool, error)
	ClaimFingerprint(context.Context) (FingerprintJob, error)
	GetFingerprintJob(context.Context, string, string) (FingerprintJob, error)
	CancelFingerprint(context.Context, string, string) (FingerprintJob, error)
	FailFingerprint(context.Context, FingerprintJob, string) error
	RecoverFingerprints(context.Context) error
	FingerprintCandidates(context.Context, HashScope, string, FingerprintRequest, int) ([]HashTarget, error)
	FingerprintIndexes(context.Context, string, string, string, int) ([]FingerprintIndex, error)
	FingerprintIndex(context.Context, string, string) (FingerprintIndex, error)
	InvalidateFingerprint(context.Context, string, string) error
}

type FingerprintSource interface {
	ResolveHashTarget(context.Context, string, string) (HashTarget, error)
	OpenStableHash(context.Context, string, string, HashTarget) (StableInput, error)
	AutoHashScopes() []HashScope
}
