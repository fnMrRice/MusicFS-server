package jobs

import (
	"context"

	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

type Job struct {
	ID                   string                               `json:"jobId"`
	Owner                string                               `json:"-"`
	ProviderInstanceID   string                               `json:"providerInstanceId"`
	Kind                 string                               `json:"-"`
	Generation           int64                                `json:"-"`
	Plan                 pluginproto.ScanPlan                 `json:"-"`
	PlanDigest           string                               `json:"-"`
	RootIDs              []string                             `json:"rootIds"`
	EffectiveOptions     pluginproto.ImportOptions            `json:"effectiveOptions"`
	EffectiveRootOptions map[string]pluginproto.ImportOptions `json:"effectiveRootOptions,omitempty"`
	SchemaRevision       string                               `json:"schemaRevision"`
	Status               string                               `json:"status"`
	Progress             struct {
		Observed int `json:"observed"`
	} `json:"progress"`
	Complete      bool          `json:"complete"`
	Error         *domain.Error `json:"error"`
	LastSuccessAt *time.Time    `json:"lastSuccessAt"`
	AttemptCount  int           `json:"attemptCount"`
	RetryCount    int           `json:"retryCount"`
	MaxAttempts   int           `json:"maxAttempts"`
	NextAttemptAt *time.Time    `json:"nextAttemptAt"`
	LastAttemptAt *time.Time    `json:"lastAttemptAt"`
	CreatedAt     time.Time     `json:"createdAt"`
	UpdatedAt     time.Time     `json:"updatedAt"`
}
type Repository interface {
	Enqueue(context.Context, string, string, pluginproto.ScanPlan, string) (Job, bool, error)
	Claim(context.Context) (Job, error)
	Finish(context.Context, Job, int, error) error
	GetJob(context.Context, string, string) (Job, error)
	RecoverJobs(context.Context) error
}
type Handler func(context.Context, Job) (int, error)
