package identification

import (
	"context"
	"time"
)

const EvidenceVersion = 1

type Range struct {
	Unit  string `json:"unit"`
	Start int64  `json:"start"`
	End   *int64 `json:"end"`
}

type Spec struct {
	Kind             string `json:"kind"`
	Origin           string `json:"origin"`
	Algorithm        string `json:"algorithm"`
	AlgorithmVersion string `json:"algorithmVersion"`
	ConfigRevision   string `json:"configRevision"`
	Range            Range  `json:"range"`
}

type Result struct {
	Status      string     `json:"status"`
	BitrateBps  *int64     `json:"bitrateBps"`
	BitrateMode string     `json:"bitrateMode"`
	SHA512      *string    `json:"sha512"`
	ComputedAt  *time.Time `json:"computedAt"`
	FailureCode *string    `json:"failureCode"`
}

type Evidence struct {
	ID              string `json:"evidenceId"`
	AssetID         string `json:"assetId"`
	AssetVersion    string `json:"assetVersion"`
	EvidenceVersion int    `json:"evidenceVersion"`
	Generation      int64  `json:"generation"`
	Spec
	Result
	InvalidatedAt *time.Time `json:"invalidatedAt"`
}

type Asset struct {
	ID              string        `json:"assetId"`
	SourceTrackID   string        `json:"sourceTrackId"`
	Version         string        `json:"version"`
	EvidenceVersion int           `json:"evidenceVersion"`
	Current         bool          `json:"current"`
	Codec           *string       `json:"codec"`
	Container       *string       `json:"container"`
	DurationMs      *int64        `json:"durationMs"`
	FileSizeBytes   *int64        `json:"fileSizeBytes"`
	BitrateBps      *int64        `json:"bitrateBps"`
	BitrateMode     string        `json:"bitrateMode"`
	BitrateOrigin   *string       `json:"bitrateOrigin"`
	ProviderSHA512  *string       `json:"providerSha512"`
	FileSHA512      *string       `json:"fileSha512"`
	HashStatus      string        `json:"hashStatus"`
	HashComputedAt  *time.Time    `json:"hashComputedAt"`
	HashConflict    *HashConflict `json:"hashConflict"`
	Evidence        []Evidence    `json:"evidence"`
}

// Binding is supplied by a trusted task owner after obtaining the current asset.
type Binding struct {
	SourceTrackID string
	AssetID       string
	AssetVersion  string
	Spec
}

// Ticket identifies a persisted pending operation; callers cannot choose its generation.
type Ticket struct {
	ID         string
	Generation int64
	Binding
}

type Repository interface {
	Asset(context.Context, string, string) (Asset, error)
	BeginEvidence(context.Context, string, Binding) (Ticket, error)
	PublishEvidence(context.Context, string, Ticket, Result) error
	CancelEvidence(context.Context, string, Ticket) error
}
