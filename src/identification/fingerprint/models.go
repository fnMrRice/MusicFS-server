package fingerprint

import (
	"context"
	"io"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/audio"
)

const AlgorithmVersion = "1.6.1"
const MaxDetailBytes = 64 << 10
const QualityRevision = "minimum-10s-unique8-dominant90-v1"

type Request = audio.NormalizeRequest
type PCMInfo = audio.PCMInfo

type Normalizer interface {
	Normalize(context.Context, Request, io.ReaderAt, int64, io.Writer) (PCMInfo, error)
}

type Config struct {
	FpcalcPath, FpcalcSHA256, FFmpegSHA256, FFmpegVersion string
	MaxConcurrent                                         int
	Timeout                                               time.Duration
}

type ToolIdentity struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

type Identity struct {
	Algorithm             string       `json:"algorithm"`
	AlgorithmVersion      string       `json:"algorithmVersion"`
	AlgorithmConfig       string       `json:"algorithmConfig"`
	ConfigRevision        string       `json:"configRevision"`
	NormalizationRevision string       `json:"normalizationRevision"`
	QualityRevision       string       `json:"qualityRevision"`
	Fpcalc                ToolIdentity `json:"fpcalc"`
	Decoder               ToolIdentity `json:"decoder"`
}

type Quality struct {
	// Status is available, short or lowInformation; none proves a match.
	Status         string `json:"status"`
	UniqueValues   int    `json:"uniqueValues"`
	DominantValues int    `json:"dominantValues"`
}

type Detail struct {
	SchemaVersion       int      `json:"schemaVersion"`
	Identity            Identity `json:"identity"`
	RequestedStartMs    int64    `json:"requestedStartMs"`
	RequestedDurationMs int64    `json:"requestedDurationMs"`
	PCM                 PCMInfo  `json:"pcm"`
	Quality             Quality  `json:"quality"`
	Values              []uint32 `json:"values"`
}
