package identification_test

import (
	"context"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/matching/standard"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/candidates"
)

type scoringFunc func(context.Context, candidates.Input, []candidates.Input) ([]candidates.Candidate, error)

func (fn scoringFunc) Find(ctx context.Context, anchor candidates.Input, others []candidates.Input) ([]candidates.Candidate, error) {
	return fn(ctx, anchor, others)
}

func TestScoringPluginOnlyReceivesEligibleSources(t *testing.T) {
	anchor := candidates.Input{Asset: candidateAsset("a")}
	blocked := candidates.Input{Asset: candidateAsset("b"), Title: strings.Repeat("x", 4097)}
	allowed := candidates.Input{Asset: candidateAsset("c")}
	called := false
	scorer := scoringFunc(func(_ context.Context, got candidates.Input, inputs []candidates.Input) ([]candidates.Candidate, error) {
		called = true
		if got.Asset != anchor.Asset || len(inputs) != 1 || inputs[0].Asset != allowed.Asset {
			t.Fatal("excluded source reached scoring plugin")
		}
		return []candidates.Candidate{}, nil
	})
	_, err := candidates.Find(context.Background(), scorer, anchor, []candidates.Input{blocked, allowed}, []candidates.Pair{{LeftSourceTrackID: "b", RightSourceTrackID: "a"}})
	if err != nil || !called {
		t.Fatalf("plugin invocation failed: %v", err)
	}
}

func TestScoringPluginCannotPublishForeignOrChangedBindings(t *testing.T) {
	anchor := candidates.Input{Asset: candidateAsset("a")}
	other := candidates.Input{Asset: candidateAsset("b")}
	for _, tc := range []struct {
		name   string
		change func(*candidates.Candidate)
	}{
		{"foreign source", func(c *candidates.Candidate) { c.Right = candidateAsset("foreign") }},
		{"changed asset", func(c *candidates.Candidate) { c.Right.AssetVersion = "changed" }},
		{"automatic confirmation", func(c *candidates.Candidate) { c.RequiresConfirmation = false }},
		{"unknown evidence version", func(c *candidates.Candidate) { c.AlgorithmVersion = "unknown" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scorer := scoringFunc(func(context.Context, candidates.Input, []candidates.Input) ([]candidates.Candidate, error) {
				c := candidates.Candidate{Left: anchor.Asset, Right: other.Asset, AlgorithmVersion: candidates.AlgorithmVersion, Score: 35, RequiresConfirmation: true, Reasons: []candidates.Reason{{Kind: "metadataOnly"}}}
				tc.change(&c)
				return []candidates.Candidate{c}, nil
			})
			if _, err := candidates.Find(context.Background(), scorer, anchor, []candidates.Input{other}, nil); err == nil {
				t.Fatal("invalid plugin result accepted")
			}
		})
	}
}

func TestNonFingerprintContractValidationRetainsStandardEvidence(t *testing.T) {
	duration := int64(120000)
	a := candidates.Input{Asset: candidateAsset("a"), Title: "Song", Artists: []string{"Artist"}, DurationMs: &duration}
	b := candidates.Input{Asset: candidateAsset("b"), Title: " song ", Artists: []string{"artist"}, DurationMs: &duration}
	for _, hashes := range []bool{false, true} {
		if hashes {
			a.FileHash = &candidates.VerifiedHash{Asset: a.Asset, EvidenceID: "hash-a", SHA512: strings.Repeat("a", 128), FileSizeBytes: 100}
			b.FileHash = &candidates.VerifiedHash{Asset: b.Asset, EvidenceID: "hash-b", SHA512: a.FileHash.SHA512, FileSizeBytes: 100}
		}
		result, err := standard.Compare(context.Background(), a, b)
		if err != nil || result == nil {
			t.Fatalf("standard evidence missing: %v", err)
		}
		if err := candidates.ValidateNonFingerprintResult(*result, a, b); err != nil {
			t.Fatalf("valid contract rejected: %v", err)
		}
		result.Score--
		if err := candidates.ValidateNonFingerprintResult(*result, a, b); err == nil {
			t.Fatal("altered score accepted")
		}
	}
}
