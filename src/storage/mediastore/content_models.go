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
	Width     int             `json:"width,omitempty"`
	Height    int             `json:"height,omitempty"`
	State     string          `json:"state"`
	CreatedAt time.Time       `json:"createdAt"`
}

// UploadInput is computed from validated bytes by the service, not trusted JSON.
type UploadInput struct {
	Metadata ContentMetadata
	MIME     string
	Size     int64
	SHA256   string
	// 尺寸由内容校验器解码图片正文取得，HTTP 不接受调用方提供这些字段。
	Width  int `json:"Width,omitempty"`
	Height int `json:"Height,omitempty"`
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
	// 在线刮削保存用户确认为本次选用的候选，其标识可在预览过期后重放。
	Online *OnlineCandidate `json:"onlineCandidate,omitempty"`
}

type OnlineCandidate struct {
	PluginID string `json:"pluginId"`
	Field    string `json:"field"`
	Token    string `json:"token"`
}

type UnlockRequest struct {
	SourceTrackID    string `json:"sourceTrackId"`
	ExpectedRevision int64  `json:"expectedRevision"`
	Field            string `json:"field"`
	Language         string `json:"language"`
}

type ContentRepository interface {
	CheckOnlineSelection(context.Context, string, SelectRequest, string) (Selection, bool, error)
	PrepareContent(context.Context, string, UploadInput, string) (Publication, bool, error)
	ContentObject(context.Context, string, string) (ContentObject, error)
	ContentSelection(context.Context, string, string) (Selection, error)
	SelectContent(context.Context, string, SelectRequest, string) (Selection, error)
	SelectContentGuarded(context.Context, string, SelectRequest, string, Guard) (Selection, error)
	UnlockContent(context.Context, string, UnlockRequest, string) (Selection, error)
	InvalidateContent(context.Context, string, string) error
}
