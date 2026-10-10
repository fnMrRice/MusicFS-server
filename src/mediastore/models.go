package mediastore

import (
	"context"
	"encoding/json"
	"time"
)

// Target describes the guarded reference transaction. Metadata is an index
// summary only; content belongs exclusively to the immutable object file.
type Target struct {
	Purpose    string          `json:"purpose"`
	ID         string          `json:"id"`
	Generation int64           `json:"generation"`
	Metadata   json.RawMessage `json:"metadata"`
}

type Publication struct {
	ID        string
	Owner     string
	Kind      string
	Status    string
	Object    Object
	Target    Target
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Repository interface {
	BeginPublication(context.Context, Publication) error
	WrittenPublication(context.Context, string, string, Object) error
	SetPublicationStage(context.Context, string, string, string, string) error
	PendingPublications(context.Context, string, int) ([]Publication, error)
	ReferencePublication(context.Context, string, string) error
	GetPublication(context.Context, string, string) (Publication, error)
	InvalidatePublication(context.Context, string, string, string) error
}

// Guard keeps the source version protected while the final DB reference changes.
type Guard func(func() error) error
