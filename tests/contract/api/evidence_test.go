package api_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func evidenceValue[T any](value T) *T { return &value }

func hashEvidence() identification.Evidence {
	return identification.Evidence{
		ID: "evidence-1", AssetID: "asset-1", AssetVersion: "version-1", EvidenceVersion: 1, Generation: 1,
		Spec: identification.Spec{Kind: "fileSha512", Origin: "probe", Algorithm: "sha512", AlgorithmVersion: "1", ConfigRevision: "1",
			Range: identification.Range{Unit: "bytes", Start: 0, End: evidenceValue(int64(33432))}},
		Result: identification.Result{Status: "verified", BitrateMode: "unknown", SHA512: evidenceValue(strings.Repeat("a", 128)),
			ComputedAt: evidenceValue(time.Date(2026, 10, 10, 1, 0, 0, 0, time.UTC))},
	}
}

func evidenceJSON(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEvidenceDTOsMatchContract(t *testing.T) {
	evidence := hashEvidence()
	testkit.ValidateHTTP(t, "AssetEvidence", evidenceJSON(t, evidence))
	evidence.Kind, evidence.Origin, evidence.Status = "providerSha512", "provider", "reported"
	testkit.ValidateHTTP(t, "AssetEvidence", evidenceJSON(t, evidence))
	evidence.Status = "invalidated"
	evidence.InvalidatedAt = evidenceValue(evidence.ComputedAt.Add(time.Minute))
	testkit.ValidateHTTP(t, "AssetEvidence", evidenceJSON(t, evidence))
	for _, origin := range []string{"provider", "probe", "estimate", "manual"} {
		evidence = hashEvidence()
		evidence.Kind, evidence.Origin, evidence.Algorithm = "bitrate", origin, "bitrate-observation"
		evidence.Status, evidence.SHA512 = "reported", nil
		evidence.BitrateBps, evidence.BitrateMode = evidenceValue(int64(44576)), "vbr"
		testkit.ValidateHTTP(t, "AssetEvidence", evidenceJSON(t, evidence))
	}
	for _, status := range []string{"pending", "failed", "unavailable"} {
		evidence = hashEvidence()
		evidence.Status, evidence.SHA512, evidence.ComputedAt = status, nil, nil
		if status != "pending" {
			evidence.FailureCode = evidenceValue("SOURCE_OFFLINE")
		}
		testkit.ValidateHTTP(t, "AssetEvidence", evidenceJSON(t, evidence))
	}
}

func TestEvidenceContractRejectsInvalidClaims(t *testing.T) {
	schema := testkit.ContractSchema(t, "http", "AssetEvidence")
	for _, test := range []struct {
		name   string
		mutate func(*identification.Evidence)
	}{
		{"provider-cannot-verify", func(e *identification.Evidence) { e.Kind, e.Origin = "providerSha512", "provider" }},
		{"wrong-full-hash-origin", func(e *identification.Evidence) { e.Origin = "manual" }},
		{"uppercase-hash", func(e *identification.Evidence) { e.SHA512 = evidenceValue(strings.Repeat("A", 128)) }},
		{"truncated-hash", func(e *identification.Evidence) { e.SHA512 = evidenceValue(strings.Repeat("a", 127)) }},
		{"partial-hash-start", func(e *identification.Evidence) { e.Range.Start = 1 }},
		{"unknown-hash-end", func(e *identification.Evidence) { e.Range.End = nil }},
		{"pcm-range-is-not-file-hash", func(e *identification.Evidence) { e.Range.Unit = "milliseconds" }},
		{"no-zero-generation", func(e *identification.Evidence) { e.Generation = 0 }},
		{"unknown-evidence-version", func(e *identification.Evidence) { e.EvidenceVersion = 2 }},
		{"private-path-not-algorithm", func(e *identification.Evidence) { e.AlgorithmVersion = "C:/private/ffprobe" }},
		{"unknown-status", func(e *identification.Evidence) { e.Status = "done" }},
		{"pending-cannot-publish-hash", func(e *identification.Evidence) { e.Status = "pending" }},
		{"failed-cannot-retain-result", func(e *identification.Evidence) { e.Status, e.FailureCode = "failed", evidenceValue("SOURCE_OFFLINE") }},
		{"success-cannot-have-failure", func(e *identification.Evidence) { e.FailureCode = evidenceValue("SOURCE_OFFLINE") }},
		{"invalidated-needs-time", func(e *identification.Evidence) { e.Status = "invalidated" }},
		{"active-cannot-be-invalidated", func(e *identification.Evidence) { e.InvalidatedAt = e.ComputedAt }},
	} {
		t.Run(test.name, func(t *testing.T) {
			evidence := hashEvidence()
			test.mutate(&evidence)
			if err := schema.Validate(testkit.DecodeJSON(t, evidenceJSON(t, evidence))); err == nil {
				t.Fatal("schema accepted invalid evidence")
			}
		})
	}
}

func TestAssetContractSeparatesUnknownPendingAndFailure(t *testing.T) {
	asset := identification.Asset{ID: "asset-1", SourceTrackID: "track-1", Version: "version-1", EvidenceVersion: 1, Current: true,
		DurationMs: evidenceValue(int64(0)), FileSizeBytes: evidenceValue(int64(0)), BitrateMode: "unknown", HashStatus: "unknown", Evidence: []identification.Evidence{}}
	testkit.ValidateHTTP(t, "SourceAsset", evidenceJSON(t, asset))
	for _, status := range []string{"pending", "failed", "unavailable"} {
		evidence := hashEvidence()
		evidence.Status, evidence.SHA512, evidence.ComputedAt = status, nil, nil
		evidence.Range.End = evidenceValue(int64(0))
		if status != "pending" {
			evidence.FailureCode = evidenceValue("SOURCE_OFFLINE")
		}
		asset.HashStatus, asset.Evidence = status, []identification.Evidence{evidence}
		testkit.ValidateHTTP(t, "SourceAsset", evidenceJSON(t, asset))
	}
	evidence := hashEvidence()
	asset.HashStatus, asset.FileSizeBytes, asset.Evidence = "verified", evidence.Range.End, []identification.Evidence{evidence}
	asset.FileSHA512, asset.HashComputedAt = evidence.SHA512, evidence.ComputedAt
	testkit.ValidateHTTP(t, "SourceAsset", evidenceJSON(t, asset))
	schema := testkit.ContractSchema(t, "http", "SourceAsset")
	for _, test := range []struct {
		name   string
		mutate func(*identification.Asset)
	}{
		{"pending-is-not-verified", func(a *identification.Asset) { a.HashStatus = "pending" }},
		{"verified-needs-digest", func(a *identification.Asset) { a.FileSHA512 = nil }},
		{"verified-needs-time", func(a *identification.Asset) { a.HashComputedAt = nil }},
		{"zero-bitrate-is-not-unknown", func(a *identification.Asset) { a.BitrateBps = evidenceValue(int64(0)) }},
		{"unknown-bitrate-has-no-origin", func(a *identification.Asset) { a.BitrateOrigin = evidenceValue("probe") }},
		{"inactive-asset-has-no-current-proof", func(a *identification.Asset) { a.Current = false }},
		{"invalidated-proof-not-current", func(a *identification.Asset) {
			a.Evidence = []identification.Evidence{evidence}
			a.Evidence[0].Status, a.Evidence[0].InvalidatedAt = "invalidated", evidence.ComputedAt
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := asset
			test.mutate(&changed)
			if err := schema.Validate(testkit.DecodeJSON(t, evidenceJSON(t, changed))); err == nil {
				t.Fatal("schema accepted invalid asset projection")
			}
		})
	}
}

func TestProviderEvidenceWireContract(t *testing.T) {
	observation := pluginproto.Observation{ExternalID: "track-1", RootID: "root-1", RelativePath: "vbr.mp3", SourceRef: "source-1",
		ObservedVersion: "version-1", DisplayName: "vbr.mp3", SizeBytes: 33432, MIME: "audio/mpeg"}
	testkit.ValidatePlugin(t, "Observation", observation)
	observation.BitrateBps, observation.BitrateMode = evidenceValue(int64(44576)), evidenceValue("vbr")
	observation.ProviderSHA512 = evidenceValue(strings.Repeat("a", 128))
	testkit.ValidatePlugin(t, "Observation", observation)
	page := pluginproto.ScanPage{ScanID: "scan-1", Generation: 1, Items: []pluginproto.Observation{observation}, Complete: true, RootIdentity: "root-version", Issues: []string{}}
	testkit.ValidatePlugin(t, "ScanPage", page)
	for _, field := range []string{"bitrateMode", "providerSha512"} {
		var incomplete map[string]any
		if err := json.Unmarshal(evidenceJSON(t, observation), &incomplete); err != nil {
			t.Fatal(err)
		}
		delete(incomplete, field)
		if err := testkit.ContractSchema(t, "plugin", "Observation").Validate(incomplete); err == nil {
			t.Fatalf("schema accepted missing %s", field)
		}
	}
}
