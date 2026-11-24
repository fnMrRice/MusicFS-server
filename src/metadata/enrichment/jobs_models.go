package enrichment

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

const MaxJobSources = 64
const MaxJobCandidates = 256
const MaxJobPreviewBytes int64 = 32 << 20
const CandidateLifetime = 30 * time.Minute

type Instance struct {
	InstanceID                string   `json:"instanceId"`
	Name                      string   `json:"name"`
	PluginID                  string   `json:"pluginId"`
	Availability              string   `json:"availability"`
	Fields                    []string `json:"fields"`
	QueryCorrectionsSupported bool     `json:"queryCorrectionsSupported"`
}

type JobRequest struct {
	SourceTrackIDs     []string            `json:"sourceTrackIds,omitempty"`
	SourceAlbum        *SourceAlbumRequest `json:"sourceAlbum,omitempty"`
	Fields             []string            `json:"fields"`
	Languages          []string            `json:"languages"`
	Query              *Query              `json:"query"`
	InstanceIDs        []string            `json:"instanceIds"`
	ExcludeInstanceIDs []string            `json:"excludeInstanceIds"`
}

// PreviewCandidate is a public projection; it contains no resource grants,
// paths, URLs, content bodies or worker-generation handles.
type PreviewCandidate struct {
	CandidateID     string    `json:"candidateId"`
	Target          Target    `json:"target"`
	InstanceID      string    `json:"instanceId"`
	Field           string    `json:"field"`
	Language        string    `json:"language"`
	Format          string    `json:"format"`
	MIME            string    `json:"mime"`
	SizeBytes       int64     `json:"sizeBytes"`
	SHA256          string    `json:"sha256"`
	Origin          string    `json:"origin"`
	Basis           string    `json:"basis"`
	Status          string    `json:"status"`
	FailureCode     *string   `json:"failureCode"`
	PreviewObjectID *string   `json:"previewObjectId"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

type JobIssue struct {
	InstanceID string `json:"instanceId"`
	Code       string `json:"code"`
}
type JobTarget struct {
	Target      Target             `json:"target"`
	State       string             `json:"state"`
	Issues      []JobIssue         `json:"issues"`
	Candidates  []PreviewCandidate `json:"candidates"`
	FailureCode *string            `json:"failureCode"`
}
type Job struct {
	JobID     string      `json:"jobId"`
	State     string      `json:"state"`
	Request   JobRequest  `json:"request"`
	Targets   []JobTarget `json:"targets"`
	CreatedAt time.Time   `json:"createdAt"`
	UpdatedAt time.Time   `json:"updatedAt"`
}

// Work is a private execution ticket. A recovered or cancelled target cannot
// publish with a prior generation, even if an old process completes later.
type Work struct {
	Owner, JobID     string
	DirectoryRefresh bool
	Target           Target
	Generation       int64
	Request          JobRequest
}
type Outcome struct {
	Candidates  []PreviewCandidate
	Issues      []JobIssue
	FailureCode *string
}

type Repository interface {
	QueueEnrichment(context.Context, string, JobRequest, string) (Job, bool, error)
	GetEnrichmentJob(context.Context, string, string) (Job, error)
	CancelEnrichment(context.Context, string, string) (Job, error)
	ClaimEnrichment(context.Context) (Work, error)
	CurrentEnrichment(context.Context, Work) error
	ReserveEnrichmentPreview(context.Context, Work, int64) error
	FinishEnrichment(context.Context, Work, Outcome) error
	RecoverEnrichment(context.Context) error
	EnrichmentCandidate(context.Context, string, string) (PreviewCandidate, error)
	ApplyEnrichment(context.Context, string, string, string, int64, string, mediastore.Guard) (mediastore.Selection, error)
}
