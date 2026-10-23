package identification_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/candidates"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprint"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

type candidateVector struct {
	Name       string   `json:"name"`
	Samples    int64    `json:"samples"`
	DurationMs int64    `json:"durationMs"`
	Values     []uint32 `json:"fingerprint"`
}

func candidateFixtures(t *testing.T) map[string]candidateVector {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "identification", "candidate-fingerprints.json"))
	if err != nil {
		t.Fatal(err)
	}
	var values []candidateVector
	if err = json.Unmarshal(b, &values); err != nil {
		t.Fatal(err)
	}
	if len(values) != 14 {
		t.Fatal("missing actual synthetic variants")
	}
	result := map[string]candidateVector{}
	for _, item := range values {
		result[item.Name] = item
	}
	return result
}

func candidateAsset(id string) candidates.AssetRef {
	return candidates.AssetRef{SourceTrackID: id, AssetID: "asset-" + id, AssetVersion: "version-1"}
}

func stampCandidateIdentity(id *fingerprint.Identity) {
	id.ConfigRevision = ""
	b, _ := json.Marshal(id)
	id.ConfigRevision = fmt.Sprintf("sha256-%x", sha256.Sum256(b))
}

func candidateInput(id string, vector candidateVector) candidates.Input {
	identity := fingerprint.Identity{Algorithm: "chromaprint", AlgorithmVersion: "1.6.1", AlgorithmConfig: "cli-2-raw-v1", NormalizationRevision: audio.NormalizationRevision, QualityRevision: fingerprint.QualityRevision,
		Analyzer: fingerprint.ToolIdentity{Version: "fpcalc version 1.6.1 (test identity)", SHA256: strings.Repeat("a", 64)},
		Decoder:  fingerprint.ToolIdentity{Version: "ffmpeg version test identity", SHA256: strings.Repeat("b", 64)}}
	stampCandidateIdentity(&identity)
	d := fingerprint.Detail{SchemaVersion: 1, Identity: identity, RequestedDurationMs: 120000,
		PCM:     audio.PCMInfo{Format: audio.FingerprintPCM, NormalizationRevision: audio.NormalizationRevision, SampleCount: vector.Samples, DecoderVersion: identity.Decoder.Version, DecoderSHA256: identity.Decoder.SHA256},
		Quality: fingerprint.Assess(vector.Values, vector.Samples), Values: append([]uint32{}, vector.Values...)}
	if vector.Name == "partial-10-30.pcm" {
		d.RequestedStartMs = 10000
		d.RequestedDurationMs = 20000
		d.PCM.StartSample = 110250
	}
	a := candidateAsset(id)
	return candidates.Input{Asset: a, DurationMs: &vector.DurationMs, Fingerprint: &candidates.FingerprintEvidence{Asset: a, EvidenceID: "fp-" + id, Detail: d}}
}

func candidateHasReason(value *candidates.Candidate, kind string) bool {
	if value == nil {
		return false
	}
	for _, reason := range value.Reasons {
		if reason.Kind == kind {
			return true
		}
	}
	return false
}
