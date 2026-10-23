package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
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

// Validate 验证公共证据身份和资源边界；具体算法与工具版本由插件负责。
func (id Identity) Validate() error {
	if id.NormalizationRevision != audio.NormalizationRevision || id.QualityRevision != QualityRevision || !validIdentityText(id.Algorithm, 128) || !validIdentityText(id.AlgorithmVersion, 128) || !validIdentityText(id.AlgorithmConfig, 128) ||
		id.ConfigRevision != id.Revision() || !validSHA(id.Decoder.SHA256) || !validSHA(id.Analyzer.SHA256) || !validIdentityText(id.Decoder.Version, 512) || !validIdentityText(id.Analyzer.Version, 512) {
		return fmt.Errorf("INVALID_ARGUMENT: invalid fingerprint identity")
	}
	return nil
}

// Revision 根据完整算法及工具身份生成稳定的证据版本。
func (id Identity) Revision() string {
	id.ConfigRevision = ""
	raw, _ := json.Marshal(id)
	sum := sha256.Sum256(raw)
	return "sha256-" + hex.EncodeToString(sum[:])
}

func validIdentityText(value string, limit int) bool {
	return value != "" && len(value) <= limit && !strings.ContainsAny(value, "\x00\r\n")
}

func validSHA(value string) bool {
	b, err := hex.DecodeString(value)
	return err == nil && len(b) == 32 && strings.ToLower(value) == value
}
