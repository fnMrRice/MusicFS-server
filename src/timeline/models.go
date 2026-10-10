// Package timeline models explicit, directed manual alignment evidence. A
// recording association or audio fingerprint is never an alignment proof.
package timeline

import (
	"context"
	"time"
)

type Target struct {
	SourceTrackID string `json:"sourceTrackId"`
	AssetID       string `json:"assetId"`
	Version       string `json:"version"`
}
type Range struct {
	StartMs int64 `json:"startMs"`
	EndMs   int64 `json:"endMs"`
}
type Profile struct {
	OutputProfile      string  `json:"outputProfile"`
	ProcessingRevision string  `json:"processingRevision"`
	DecoderSHA256      string  `json:"decoderSha256"`
	GainDB             float64 `json:"gainDb"`
}
type Request struct {
	RecordingID string  `json:"recordingId"`
	From        Target  `json:"from"`
	To          Target  `json:"to"`
	Range       Range   `json:"range"`
	OffsetMs    int64   `json:"offsetMs"`
	Profile     Profile `json:"profile"`
}
type Evidence struct {
	ID string `json:"evidenceId"`
	Request
	Origin    string     `json:"origin"`
	Revision  int64      `json:"revision"`
	CreatedAt time.Time  `json:"createdAt"`
	RevokedAt *time.Time `json:"revokedAt"`
}
type Check struct {
	From, To   Target
	EvidenceID string
	PositionMs int64
	Profile    Profile
}
type Repository interface {
	CreateTimeline(context.Context, string, Request, string) (Evidence, error)
	Timeline(context.Context, string, string) (Evidence, error)
	RevokeTimeline(context.Context, string, string, int64, string) (Evidence, error)
	CheckTimeline(context.Context, string, Check) (Evidence, error)
	CommitRecovery(context.Context, Commit) error
}

// Commit is an internal, trusted publication contract. The repository checks
// both current assets, owner, current playback attempt, auth and evidence in
// one transaction before publishing the new attempt and idempotent result.
type Commit struct {
	SessionID, AuthID, Owner, PreviousAttemptID string
	Check                                       Check
	Continue                                    bool
	Description, Result                         []byte
	ExpiresAt                                   time.Time
	Route, Key, Digest                          string
}
