// Package writebackhost coordinates explicit writes with durable jobs and library readers.
package writebackhost

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writeback"
)

type Field struct {
	Name     string `json:"name"`
	Language string `json:"language"`
}

// Request contains selected fields, never a filesystem path or replacement bytes.
type Request struct {
	SourceTrackID    string  `json:"sourceTrackId"`
	AssetID          string  `json:"assetId"`
	AssetVersion     string  `json:"assetVersion"`
	ExpectedRevision int64   `json:"expectedRevision"`
	Fields           []Field `json:"fields"`
	Overwrite        bool    `json:"overwrite"`
}

type Outcome struct {
	Changed         bool   `json:"changed"`
	PreviousAssetID string `json:"previousAssetId"`
	AssetID         string `json:"assetId"`
	AssetVersion    string `json:"assetVersion"`
	FileSHA512      string `json:"fileSha512"`
}

type Job struct {
	ID        string        `json:"jobId"`
	Owner     string        `json:"-"`
	Request   Request       `json:"request"`
	Status    string        `json:"status"`
	Error     *domain.Error `json:"error"`
	Result    *Outcome      `json:"result"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

type ObjectRef struct {
	Field    Field
	ObjectID string
}

// Work is private persistence. Public responses expose only Job.
type Work struct {
	Job        Job
	Target     writeback.Target
	ExternalID string
	Objects    []ObjectRef
}

type Repository interface {
	ReplayWriteback(context.Context, string, Request, string) (Job, bool, error)
	QueueWriteback(context.Context, string, Request, writeback.Target, string) (Job, bool, error)
	GetWriteback(context.Context, string, string) (Job, error)
	NextWriteback(context.Context) (Work, error)
	DeferWriteback(context.Context, string, string) error
	WritebackWork(context.Context, string, string) (Work, error)
	// Begin and Check validate exact source/selection and retain the mutation fence.
	BeginWriteback(context.Context, string, string, writeback.Target, int64) error
	CheckWriteback(context.Context, string, string, writeback.Target, int64) (bool, error)
	CommitWriteback(context.Context, string, string, writeback.Result, library.Observation) (Outcome, error)
	SetWritebackState(context.Context, string, string, string, string) error
	CancelWriteback(context.Context, string, string) (Job, error)
	// Restart before any workers: running jobs without a journal return to queued.
	RecoverWritebackQueue(context.Context, map[string]bool) error
}
