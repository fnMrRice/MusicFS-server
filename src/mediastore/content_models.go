package mediastore

import (
	"context"
	"time"
)

// ContentTarget binds content to one source version, independently of Recording.
type ContentTarget struct {
	SourceTrackID string `json:"sourceTrackId"`
	AssetID       string `json:"assetId"`
	AssetVersion  string `json:"assetVersion"`
}

// Origin is provenance, never a private path, URL, credential or content body.
type Origin struct {
	Kind        string `json:"kind"`
	InstanceID  string `json:"instanceId"`
	CandidateID string `json:"candidateId"`
}

type ContentMetadata struct {
	Target   ContentTarget `json:"target"`
	Field    string        `json:"field"`
	Language string        `json:"language"`
	Format   string        `json:"format"`
	Origin   Origin        `json:"origin"`
}

type ContentObject struct {
	Object
	Metadata  ContentMetadata `json:"metadata"`
	State     string          `json:"state"`
	CreatedAt time.Time       `json:"createdAt"`
}

// UploadInput is computed from validated bytes by the service, not trusted JSON.
type UploadInput struct {
	Metadata ContentMetadata
	MIME     string
	Size     int64
	SHA256   string
}

type Binding struct {
	ID           string        `json:"bindingId"`
	ObjectID     string        `json:"objectId"`
	Target       ContentTarget `json:"target"`
	Field        string        `json:"field"`
	Language     string        `json:"language"`
	ManualLocked bool          `json:"manualLocked"`
	Current      bool          `json:"current"`
	State        string        `json:"state"`
	CreatedAt    time.Time     `json:"createdAt"`
}

type Selection struct {
	Target   ContentTarget `json:"target"`
	Revision int64         `json:"revision"`
	Bindings []Binding     `json:"bindings"`
}

type SelectRequest struct {
	Target           ContentTarget `json:"target"`
	ExpectedRevision int64         `json:"expectedRevision"`
	ObjectID         string        `json:"objectId"`
	// Automatic is service-owned. HTTP manual selection must set it to false.
	Automatic bool `json:"-"`
}

type UnlockRequest struct {
	SourceTrackID    string `json:"sourceTrackId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Field            string `json:"field"`
	Language         string `json:"language"`
}

type ContentRepository interface {
	PrepareContent(context.Context, string, UploadInput, string) (Publication, bool, error)
	ContentObject(context.Context, string, string) (ContentObject, error)
	ContentSelection(context.Context, string, string) (Selection, error)
	SelectContent(context.Context, string, SelectRequest, string) (Selection, error)
	SelectContentGuarded(context.Context, string, SelectRequest, string, Guard) (Selection, error)
	UnlockContent(context.Context, string, UnlockRequest, string) (Selection, error)
	InvalidateContent(context.Context, string, string) error
}
