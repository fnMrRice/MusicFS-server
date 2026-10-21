// Package matching preserves owner-scoped recording decisions and candidate
// evidence. It never merges source permissions or creates timeline evidence.
package matching

import (
	"time"

	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/candidates"
)

const MaxSources = 256

// CandidateSource is trusted service input, never accepted from HTTP JSON.
type CandidateSource struct {
	Input           candidates.Input
	Fingerprints    []identification.FingerprintIndex
	BindingRevision string
}

// Sources is an explicit, bounded comparison pool. The metadata snapshot and
// fingerprint binding revisions are checked again before candidate publication.
type Snapshot struct {
	Owner           string
	Revision        int64
	LibraryRevision int64
	Anchor          CandidateSource
	Sources         []CandidateSource
	Blocked         []candidates.Pair
}

type Candidate struct {
	ID string `json:"candidateId"`
	candidates.Candidate
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

type CandidatePage struct {
	Revision  int64       `json:"revision"`
	Items     []Candidate `json:"items"`
	NextAfter *string     `json:"nextAfter"`
}

type Recording struct {
	ID             string    `json:"recordingId"`
	Revision       int64     `json:"revision"`
	SourceTrackIDs []string  `json:"sourceTrackIds"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type Link struct {
	SourceTrackID string  `json:"sourceTrackId"`
	RecordingID   *string `json:"recordingId"`
	Decision      string  `json:"decision"`
	Revision      int64   `json:"revision"`
}

type LinkRequest struct {
	ExpectedRevision  int64    `json:"expectedRevision"`
	SourceTrackIDs    []string `json:"sourceTrackIds"`
	TargetRecordingID *string  `json:"targetRecordingId"`
}

type UnlinkRequest struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	SourceTrackID    string `json:"sourceTrackId"`
}

type SplitRequest struct {
	ExpectedRevision int64    `json:"expectedRevision"`
	RecordingID      string   `json:"recordingId"`
	SourceTrackIDs   []string `json:"sourceTrackIds"`
}

type ExclusionRequest struct {
	ExpectedRevision   int64  `json:"expectedRevision"`
	LeftSourceTrackID  string `json:"leftSourceTrackId"`
	RightSourceTrackID string `json:"rightSourceTrackId"`
	Excluded           bool   `json:"excluded"`
}

// LinkChange preserves original targets for later reversible hidden-intent
// migration. Exclusion changes use the pair fields; source moves use from/to.
type LinkChange struct {
	Sequence           int64     `json:"sequence"`
	OperationID        string    `json:"operationId"`
	Operation          string    `json:"operation"`
	Owner              string    `json:"-"`
	Revision           int64     `json:"revision"`
	SourceTrackID      *string   `json:"sourceTrackId"`
	FromRecordingID    *string   `json:"fromRecordingId"`
	ToRecordingID      *string   `json:"toRecordingId"`
	LeftSourceTrackID  *string   `json:"leftSourceTrackId"`
	RightSourceTrackID *string   `json:"rightSourceTrackId"`
	Excluded           *bool     `json:"excluded"`
	CreatedAt          time.Time `json:"createdAt"`
}

type Mutation struct {
	Revision    int64        `json:"revision"`
	OperationID string       `json:"operationId"`
	RecordingID *string      `json:"recordingId"`
	Changes     []LinkChange `json:"changes"`
}

type ChangePage struct {
	Revision  int64        `json:"revision"`
	Items     []LinkChange `json:"items"`
	NextAfter *int64       `json:"nextAfter"`
}
