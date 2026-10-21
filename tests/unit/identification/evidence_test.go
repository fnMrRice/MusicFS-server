package identification_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
)

func TestEvidenceSpecAndResultRejectFalseProofs(t *testing.T) {
	size := int64(100)
	spec := identification.Spec{Kind: "fileSha512", Origin: "probe", Algorithm: "sha512", AlgorithmVersion: "1", ConfigRevision: "full-file-1", Range: identification.Range{Unit: "bytes", End: &size}}
	if err := spec.Validate(&size); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*identification.Spec){
		"partial range":           func(s *identification.Spec) { s.Range.Start = 1 },
		"unknown range end":       func(s *identification.Spec) { s.Range.End = nil },
		"audio payload scope":     func(s *identification.Spec) { s.Range.Unit = "milliseconds" },
		"provider verification":   func(s *identification.Spec) { s.Origin = "provider" },
		"different algorithm":     func(s *identification.Spec) { s.Algorithm = "etag" },
		"unknown version":         func(s *identification.Spec) { s.AlgorithmVersion = "" },
		"private path in version": func(s *identification.Spec) { s.ConfigRevision = "/private/source" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := spec
			mutate(&changed)
			if changed.Validate(&size) == nil {
				t.Fatal("unsafe evidence spec accepted")
			}
		})
	}
	now := time.Now().UTC()
	digest := strings.Repeat("a", 128)
	result := identification.Result{Status: "verified", BitrateMode: "unknown", SHA512: &digest, ComputedAt: &now}
	if err := result.Validate(spec); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*identification.Result){
		"missing digest":                 func(r *identification.Result) { r.SHA512 = nil },
		"uppercase digest":               func(r *identification.Result) { v := strings.Repeat("A", 128); r.SHA512 = &v },
		"short digest":                   func(r *identification.Result) { v := "abc"; r.SHA512 = &v },
		"missing computed time":          func(r *identification.Result) { r.ComputedAt = nil },
		"future time":                    func(r *identification.Result) { v := now.Add(time.Hour); r.ComputedAt = &v },
		"result masquerading as pending": func(r *identification.Result) { r.Status = "pending" },
		"failed result with digest":      func(r *identification.Result) { r.Status = "failed"; v := "UNSUPPORTED"; r.FailureCode = &v },
		"hash with bitrate":              func(r *identification.Result) { v := int64(128000); r.BitrateBps = &v },
	} {
		t.Run(name, func(t *testing.T) {
			changed := result
			mutate(&changed)
			if changed.Validate(spec) == nil {
				t.Fatal("invalid proof accepted")
			}
		})
	}
	provider := spec
	provider.Kind, provider.Origin = "providerSha512", "provider"
	if result.Validate(provider) == nil {
		t.Fatal("provider marked server digest verified")
	}
}

func TestObservationRejectsBogusValuesAndDoesNotInventEvidence(t *testing.T) {
	o := library.Observation{Observation: pluginproto.Observation{SizeBytes: 0}}
	unknown, err := library.EvidenceForObservation(o)
	if err != nil || len(unknown) != 0 {
		t.Fatal("unknown values became evidence", err)
	}
	for _, rate := range []int64{0, -1} {
		o.BitrateBps = &rate
		if _, err := library.EvidenceForObservation(o); err == nil {
			t.Fatal("nonpositive bitrate accepted")
		}
	}
	rate := int64(200000)
	mode := "vbr"
	o.BitrateBps, o.BitrateMode = &rate, &mode
	evidence, err := library.EvidenceForObservation(o)
	if err != nil || len(evidence) != 1 || evidence[0].Origin != "provider" || evidence[0].BitrateMode != "vbr" {
		t.Fatal("VBR declaration lost", err)
	}
	mode = "lossless-quality"
	if _, err := library.EvidenceForObservation(o); err == nil {
		t.Fatal("quality name became bitrate mode")
	}
	mode = "unknown"
	badDigest := "etag-value"
	o.ProviderSHA512 = &badDigest
	if _, err := library.EvidenceForObservation(o); err == nil {
		t.Fatal("provider ETag became SHA-512")
	}
	a := identification.Asset{ID: "asset", Current: true}
	a.Project(nil)
	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"bitrateBps":null`, `"bitrateOrigin":null`, `"providerSha512":null`, `"fileSha512":null`, `"hashComputedAt":null`, `"hashStatus":"unknown"`, `"evidence":[]`} {
		if !strings.Contains(string(raw), field) {
			t.Fatalf("missing explicit unknown value %s", field)
		}
	}
}

func TestActualToolVersionIdentitySupportsDistributionVersions(t *testing.T) {
	canonical := "6.1.1-full_build-www.gyan.dev"
	if identification.AlgorithmVersionID(canonical) != canonical {
		t.Fatal("canonical version changed")
	}
	versions := []string{"5.1.6-0+deb12u1", "5.1.6-0deb12u1", "vendor build 5.1", strings.Repeat("version", 40)}
	seen := map[string]bool{}
	for _, version := range versions {
		id := identification.AlgorithmVersionID(version)
		if seen[id] {
			t.Fatal("distinct tool versions collided after normalization")
		}
		seen[id] = true
		spec := identification.Spec{Kind: "bitrate", Origin: "probe", Algorithm: "ffprobe", AlgorithmVersion: id, ConfigRevision: "probe-v1", Range: identification.Range{Unit: "milliseconds"}}
		if err := spec.Validate(nil); err != nil {
			t.Fatalf("distribution version cannot be recorded: %v", err)
		}
	}
}
