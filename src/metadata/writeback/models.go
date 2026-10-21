// Package writeback implements explicitly authorized, recoverable local edits.
// It never enables writing implicitly and does not implement audio transcoding.
package writeback

import (
	"context"
	"time"
)

type Root struct {
	Owner, InstanceID, RootID, Path, Identity string
	Fields                                    []string
}
type Policy struct {
	Enabled      bool
	Roots        []Root
	MaxFileBytes int64
}
type Target struct {
	SourceTrackID string `json:"sourceTrackId"`
	AssetID       string `json:"assetId"`
	AssetVersion  string `json:"assetVersion"`
	InstanceID    string `json:"instanceId"`
	RootID        string `json:"rootId"`
	RelativePath  string `json:"relativePath"`
	FileIdentity  string `json:"fileIdentity"`
}

// Patch only sets supplied nonempty values. Existing values are retained unless
// Overwrite is explicit. Comments supports title, artists, album, and lyrics.
// Artwork is a bounded, decoded PNG/JPEG front cover; nil leaves all art intact.
type Patch struct {
	Comments  map[string][]string `json:"comments"`
	Artwork   []byte              `json:"artwork,omitempty"`
	Overwrite bool                `json:"overwrite"`
}
type Request struct {
	ID                string `json:"id"`
	Target            Target `json:"target"`
	SelectionRevision int64  `json:"selectionRevision"`
	Patch             Patch  `json:"patch"`
}
type FileState struct {
	Identity string `json:"identity"`
	Version  string `json:"version"`
	SHA512   string `json:"sha512"`
	Size     int64  `json:"size"`
}
type Result struct {
	JobID, SourceTrackID string
	Old, New             FileState
	AudioSHA512          string
	BackupRelativePath   string
	Changed              bool
}

// Record is trusted journal data, not an API response: it contains authorized
// paths but never the patch body. The prepared intent precedes all file writes.
type Record struct {
	ID                string    `json:"id"`
	Owner             string    `json:"owner"`
	Target            Target    `json:"target"`
	SelectionRevision int64     `json:"selectionRevision"`
	RequestDigest     string    `json:"requestDigest"`
	Fields            []string  `json:"fields"`
	RootIdentity      string    `json:"rootIdentity"`
	Stage             string    `json:"stage"`
	Old               FileState `json:"old"`
	New               FileState `json:"new"`
	AudioSHA512       string    `json:"audioSha512"`
	Changed           bool      `json:"changed"`
	CreatedAt         time.Time `json:"createdAt"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

// Journal must durably compare-and-swap stage and prevent duplicate IDs. A
// successful Save means the intent survives a process restart. Implementations
// must retain terminal records and restrict journal access to trusted code.
type Journal interface {
	Create(context.Context, Record) error
	Load(context.Context, string) (Record, error)
	Save(context.Context, string, Record) error
	Pending(context.Context, string, int) ([]Record, error)
}

// Coordinator acquires the host's exclusive source/root coordination before
// any file access. It must reject active playback, scans and hash jobs, prevent
// new ones, and verify owner, exact target binding and selection revision.
// Recover acquires the same lock but accepts a committed idempotent job whose
// database asset already reflects Record.New. It never trusts caller paths.
type Coordinator interface {
	Acquire(context.Context, string, Target, int64, string) (Lease, error)
	Recover(context.Context, Record) (Lease, error)
}
type Lease interface {
	Check(context.Context) error
	// Commit must atomically and idempotently publish the new asset/identity,
	// invalidate old evidence, and preserve the stable SourceTrack identity.
	Commit(context.Context, Result) error
	Close() error
}
