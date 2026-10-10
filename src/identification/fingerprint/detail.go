package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/audio"
)

// Assess is a conservative information gate, not an estimated match probability.
func Assess(values []uint32, samples int64) Quality {
	q := Quality{Status: "available"}
	counts := map[uint32]int{}
	for _, v := range values {
		counts[v]++
		if counts[v] > q.DominantValues {
			q.DominantValues = counts[v]
		}
	}
	q.UniqueValues = len(counts)
	if samples < 110250 {
		q.Status = "short"
	} else if q.UniqueValues < 8 || len(values) == 0 || q.DominantValues*10 >= len(values)*9 {
		q.Status = "lowInformation"
	}
	return q
}

func (d Detail) Encode() ([]byte, error) {
	start, count, err := (Request{TaskID: "detail", AssetID: "detail", StartMs: d.RequestedStartMs, DurationMs: d.RequestedDurationMs}).Samples()
	id := d.Identity
	if err != nil || d.SchemaVersion != 1 || d.Values == nil || len(d.Values) > 1000 || d.PCM.SampleCount <= 0 || d.PCM.SampleCount > count || d.PCM.StartSample != start ||
		d.PCM.Format != audio.FingerprintPCM || d.PCM.NormalizationRevision != audio.NormalizationRevision || id.Validate() != nil ||
		d.PCM.DecoderSHA256 != id.Decoder.SHA256 || d.PCM.DecoderVersion != id.Decoder.Version ||
		d.Quality != Assess(d.Values, d.PCM.SampleCount) {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid fingerprint detail")
	}
	raw, err := json.Marshal(d)
	if err != nil || len(raw) > MaxDetailBytes {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: fingerprint detail exceeds limit")
	}
	return raw, nil
}

// Validate checks the complete pinned algorithm identity without running tools.
func (id Identity) Validate() error {
	if id.NormalizationRevision != audio.NormalizationRevision || id.QualityRevision != QualityRevision || id.Algorithm != "chromaprint" || id.AlgorithmVersion != AlgorithmVersion || id.AlgorithmConfig != "cli-2-raw-v1" ||
		id.ConfigRevision != identityRevision(id) || !validSHA(id.Decoder.SHA256) || !validSHA(id.Fpcalc.SHA256) || id.Decoder.Version == "" || len(id.Decoder.Version) > 512 ||
		len(id.Fpcalc.Version) > 512 || strings.ContainsAny(id.Fpcalc.Version, "\r\n") || !strings.HasPrefix(id.Fpcalc.Version, "fpcalc version 1.6.1 (") || !strings.HasSuffix(id.Fpcalc.Version, ")") {
		return fmt.Errorf("INVALID_ARGUMENT: invalid fingerprint identity")
	}
	return nil
}

func identityRevision(id Identity) string {
	id.ConfigRevision = ""
	raw, _ := json.Marshal(id)
	sum := sha256.Sum256(raw)
	return "sha256-" + hex.EncodeToString(sum[:])
}
