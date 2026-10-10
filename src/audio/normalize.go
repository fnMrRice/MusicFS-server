package audio

import "fmt"

const NormalizationRevision = "s16le-mono-11025-floor-range-v1"

var FingerprintPCM = Format{SampleFormat: "s16le", SampleRate: 11025, Channels: 1, Layout: "mono"}

type NormalizeRequest struct {
	TaskID     string `json:"taskId"`
	AssetID    string `json:"assetId"`
	StartMs    int64  `json:"startMs"`
	DurationMs int64  `json:"durationMs"`
}

// Samples quantizes both absolute endpoints; rounding the duration separately
// would sometimes underestimate the permitted output by one sample.
func (r NormalizeRequest) Samples() (int64, int64, error) {
	if r.TaskID == "" || r.AssetID == "" || len(r.TaskID) > 128 || len(r.AssetID) > 128 || r.StartMs < 0 || r.StartMs > 24*60*60*1000 || r.DurationMs < 10000 || r.DurationMs > 120000 {
		return 0, 0, fmt.Errorf("INVALID_ARGUMENT: invalid normalization range")
	}
	start := r.StartMs * 11025 / 1000
	end := (r.StartMs + r.DurationMs) * 11025 / 1000
	return start, end - start, nil
}

type PCMInfo struct {
	Format                Format `json:"format"`
	NormalizationRevision string `json:"normalizationRevision"`
	StartSample           int64  `json:"startSample"`
	SampleCount           int64  `json:"sampleCount"`
	DecoderVersion        string `json:"decoderVersion"`
	DecoderSHA256         string `json:"decoderSHA256"`
}
