// Package candidates ranks local evidence for human review. It never links
// recordings, removes sources, or produces playback timeline evidence.
package candidates

import "github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprint"

const AlgorithmVersion = "local-candidates-v1"
const MaxInputs = 256
const MaxResults = 20
const MaxPopcounts = 13_000_000

type AssetRef struct {
	SourceTrackID string `json:"sourceTrackId"`
	AssetID       string `json:"assetId"`
	AssetVersion  string `json:"assetVersion"`
}

// VerifiedHash may only be constructed from the server's current verified
// complete-file evidence. Provider claims and decoded PCM hashes do not qualify.
type VerifiedHash struct {
	Asset         AssetRef
	EvidenceID    string
	SHA512        string
	FileSizeBytes int64
}

type FingerprintEvidence struct {
	Asset      AssetRef
	EvidenceID string
	Detail     fingerprint.Detail
}

// Input contains an already authorized current asset. The caller obtains these
// bindings and evidence from trusted storage, not directly from request JSON.
type Input struct {
	Asset       AssetRef
	Title       string
	Artists     []string
	DurationMs  *int64
	FileHash    *VerifiedHash
	Fingerprint *FingerprintEvidence
}

// Pair blocks both directions. Callers include exclusions and manual conflicts.
type Pair struct {
	LeftSourceTrackID  string
	RightSourceTrackID string
}

type FingerprintComparison struct {
	BitErrors     int `json:"bitErrors"`
	ComparedBits  int `json:"comparedBits"`
	ComparedItems int `json:"comparedItems"`
	// OffsetItems is a search index, never a playback time mapping.
	OffsetItems int     `json:"offsetItems"`
	BER         float64 `json:"ber"`
}

type Reason struct {
	Kind                 string                 `json:"kind"`
	LeftEvidenceID       string                 `json:"leftEvidenceId,omitempty"`
	RightEvidenceID      string                 `json:"rightEvidenceId,omitempty"`
	Fingerprint          *FingerprintComparison `json:"fingerprint,omitempty"`
	DurationDifferenceMs *int64                 `json:"durationDifferenceMs,omitempty"`
}

type Candidate struct {
	Left             AssetRef `json:"left"`
	Right            AssetRef `json:"right"`
	AlgorithmVersion string   `json:"algorithmVersion"`
	// Score is an ordinal review priority, not a probability or confidence level.
	Score                int      `json:"score"`
	RequiresConfirmation bool     `json:"requiresConfirmation"`
	Reasons              []Reason `json:"reasons"`
}
