package identification_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprint"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

func TestFingerprintIdentityRejectsIncompatibleAndAlteredEvidence(t *testing.T) {
	id := fingerprint.Identity{Algorithm: "chromaprint", AlgorithmVersion: "1.6.1", AlgorithmConfig: "cli-2-raw-v1", NormalizationRevision: audio.NormalizationRevision, QualityRevision: fingerprint.QualityRevision,
		Analyzer: fingerprint.ToolIdentity{Version: "fpcalc version 1.6.1 (test)", SHA256: strings.Repeat("a", 64)},
		Decoder:  fingerprint.ToolIdentity{Version: "ffmpeg version test", SHA256: strings.Repeat("b", 64)}}
	stamp := func(value *fingerprint.Identity) {
		value.ConfigRevision = ""
		b, _ := json.Marshal(value)
		value.ConfigRevision = fmt.Sprintf("sha256-%x", sha256.Sum256(b))
	}
	stamp(&id)
	if err := id.Validate(); err != nil {
		t.Fatal(err)
	}
	changed := id
	changed.Decoder.Version += " changed"
	if changed.Validate() == nil {
		t.Fatal("stale revision accepted")
	}
	for _, mutate := range []func(*fingerprint.Identity){
		func(v *fingerprint.Identity) { v.AlgorithmVersion = "" },
		func(v *fingerprint.Identity) { v.AlgorithmConfig = "" },
		func(v *fingerprint.Identity) { v.Decoder.SHA256 = strings.Repeat("B", 64) },
		func(v *fingerprint.Identity) { v.Analyzer.Version = "invalid\nversion" },
		func(v *fingerprint.Identity) { v.NormalizationRevision = "unknown" },
		func(v *fingerprint.Identity) { v.QualityRevision = "unknown" },
	} {
		changed = id
		mutate(&changed)
		stamp(&changed)
		if changed.Validate() == nil {
			t.Fatal("unsupported algorithm identity accepted")
		}
	}
}

func TestFingerprintRangeRoundingAndLimits(t *testing.T) {
	for _, tc := range []struct{ start, duration, first, count int64 }{{1, 10000, 11, 110250}, {7, 10007, 77, 110327}, {30, 10030, 330, 110581}, {0, 120000, 0, 1323000}} {
		r := audio.NormalizeRequest{TaskID: "task", AssetID: "asset", StartMs: tc.start, DurationMs: tc.duration}
		first, count, err := r.Samples()
		if err != nil || first != tc.first || count != tc.count {
			t.Fatalf("range (%d,%d): %d %d %v", tc.start, tc.duration, first, count, err)
		}
	}
	for _, r := range []audio.NormalizeRequest{{TaskID: "task", AssetID: "asset", DurationMs: 9999}, {TaskID: "task", AssetID: "asset", DurationMs: 120001}, {TaskID: "task", AssetID: "asset", StartMs: -1, DurationMs: 10000}, {TaskID: "task", AssetID: "asset", StartMs: 1 << 62, DurationMs: 10000}, {DurationMs: 10000}} {
		if _, _, err := r.Samples(); err == nil {
			t.Fatal("invalid or overflowing range accepted")
		}
	}
}

func TestFingerprintIdentityAcceptsIndependentAnalyzerAndRelease(t *testing.T) {
	id := fingerprint.Identity{Algorithm: "independent-fingerprint", AlgorithmVersion: "3.2.0", AlgorithmConfig: "raw-words-v2", NormalizationRevision: audio.NormalizationRevision, QualityRevision: fingerprint.QualityRevision,
		Analyzer: fingerprint.ToolIdentity{Version: "native-analyzer 3.2.0", SHA256: strings.Repeat("a", 64)}, Decoder: fingerprint.ToolIdentity{Version: "native-decoder 2", SHA256: strings.Repeat("b", 64)}}
	id.ConfigRevision = id.Revision()
	if err := id.Validate(); err != nil {
		t.Fatal(err)
	}
	upgraded := id
	upgraded.AlgorithmVersion = "3.3.0"
	upgraded.Analyzer.Version = "native-analyzer 3.3.0"
	upgraded.ConfigRevision = upgraded.Revision()
	if err := upgraded.Validate(); err != nil || upgraded.ConfigRevision == id.ConfigRevision {
		t.Fatal("analyzer release was not independently versioned", err)
	}
}

func TestFingerprintRejectsInsufficientAndLowInformation(t *testing.T) {
	constant := make([]uint32, 100)
	for i := range constant {
		constant[i] = 0xaaaaaaaa
	}
	if q := fingerprint.Assess(constant, 110250); q.Status != "lowInformation" || q.UniqueValues != 1 {
		t.Fatal("constant fingerprint became usable evidence")
	}
	var varied []uint32
	for i := 0; i < 100; i++ {
		varied = append(varied, uint32(i))
	}
	if q := fingerprint.Assess(varied, 110249); q.Status != "short" {
		t.Fatal("short audio became usable evidence")
	}
	if q := fingerprint.Assess(varied, 110250); q.Status != "available" {
		t.Fatal("informative window was rejected")
	}
	copy(constant[:10], varied[:10])
	if q := fingerprint.Assess(constant, 220500); q.Status != "lowInformation" {
		t.Fatal("dominant repeated value was accepted")
	}
}
