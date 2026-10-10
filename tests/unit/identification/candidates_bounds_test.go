package identification_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/identification/candidates"
	"github.com/fnMrRice/MusicFS-server/src/identification/fingerprint"
)

func TestCandidatesFingerprintBindingAndCompatibility(t *testing.T) {
	vector := candidateFixtures(t)["original.pcm"]
	a := candidateInput("a", vector)
	for _, tc := range []struct {
		name    string
		mutate  func(*candidates.Input)
		invalid bool
	}{
		{"assetChanged", func(v *candidates.Input) { v.Asset.AssetVersion = "new" }, false},
		{"wrongTrack", func(v *candidates.Input) { v.Fingerprint.Asset.SourceTrackID = "other" }, false},
		{"differentTool", func(v *candidates.Input) {
			d := &v.Fingerprint.Detail
			d.Identity.Decoder.Version = "ffmpeg version changed"
			d.PCM.DecoderVersion = d.Identity.Decoder.Version
			stampCandidateIdentity(&d.Identity)
		}, false},
		{"differentStart", func(v *candidates.Input) {
			d := &v.Fingerprint.Detail
			d.RequestedStartMs = 1000
			d.PCM.StartSample = 11025
		}, false},
		{"differentRequestLength", func(v *candidates.Input) { v.Fingerprint.Detail.RequestedDurationMs = 36000 }, false},
		{"differentActualCoverage", func(v *candidates.Input) { v.Fingerprint.Detail.PCM.SampleCount = 110250 }, false},
		{"short", func(v *candidates.Input) {
			d := &v.Fingerprint.Detail
			d.PCM.SampleCount = 22050
			d.Quality = fingerprint.Assess(d.Values, d.PCM.SampleCount)
		}, false},
		{"unrecognizedAlgorithm", func(v *candidates.Input) {
			d := &v.Fingerprint.Detail
			d.Identity.AlgorithmVersion = "future"
			stampCandidateIdentity(&d.Identity)
		}, true},
		{"falseQuality", func(v *candidates.Input) { v.Fingerprint.Detail.Quality.Status = "short" }, true},
		{"tooManyItems", func(v *candidates.Input) {
			d := &v.Fingerprint.Detail
			d.Values = make([]uint32, 1001)
			d.Quality = fingerprint.Assess(d.Values, d.PCM.SampleCount)
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := candidateInput("b", vector)
			tc.mutate(&b)
			got, err := candidates.Compare(context.Background(), a, b)
			if got != nil || (err != nil) != tc.invalid {
				t.Fatalf("unexpected evidence acceptance: %+v %v", got, err)
			}
		})
	}
}

func TestCandidatesFindExclusionsRankingAndLimits(t *testing.T) {
	anchor := candidates.Input{Asset: candidateAsset("anchor")}
	anchor.FileHash = &candidates.VerifiedHash{Asset: anchor.Asset, EvidenceID: "hash-anchor", SHA512: strings.Repeat("e", 128)}
	others := []candidates.Input{}
	for i := 0; i < 32; i++ {
		item := candidates.Input{Asset: candidateAsset(fmt.Sprintf("source-%03d", 31-i))}
		item.FileHash = &candidates.VerifiedHash{Asset: item.Asset, EvidenceID: "hash-" + item.Asset.SourceTrackID, SHA512: anchor.FileHash.SHA512}
		others = append(others, item)
	}
	blocked := []candidates.Pair{{LeftSourceTrackID: "source-000", RightSourceTrackID: "anchor"}, {LeftSourceTrackID: "anchor", RightSourceTrackID: "source-001"}}
	// A blocked source is discarded before malformed data can affect the result.
	others[31].Title = strings.Repeat("x", 4097)
	got, err := candidates.Find(context.Background(), anchor, others, blocked)
	if err != nil || len(got) != 20 {
		t.Fatalf("bounded candidate set: %d %v", len(got), err)
	}
	for i, item := range got {
		if item.Right.SourceTrackID != fmt.Sprintf("source-%03d", i+2) || item.Score != 100 || !item.RequiresConfirmation {
			t.Fatal("exclusion priority or deterministic ranking changed")
		}
	}
	if _, err = candidates.Find(context.Background(), anchor, []candidates.Input{others[0], others[0]}, nil); err == nil {
		t.Fatal("duplicate sources accepted")
	}
	if _, err = candidates.Find(context.Background(), anchor, make([]candidates.Input, 257), nil); err == nil {
		t.Fatal("input budget ignored")
	}
	if _, err = candidates.Find(context.Background(), anchor, nil, make([]candidates.Pair, 4097)); err == nil {
		t.Fatal("exclusion budget ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = candidates.Find(ctx, anchor, others, blocked); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled search continued")
	}
	if _, err = candidates.Compare(ctx, anchor, others[0]); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled comparison continued")
	}
}

func TestCandidatesMaximumBoundedFingerprintBatch(t *testing.T) {
	values := make([]uint32, 1000)
	var value uint32 = 0x12345678
	for i := range values {
		value ^= value << 13
		value ^= value >> 17
		value ^= value << 5
		values[i] = value
	}
	vector := candidateVector{Samples: 1323000, DurationMs: 120000, Values: values}
	anchor := candidateInput("anchor", vector)
	others := make([]candidates.Input, 256)
	for i := range others {
		others[i] = candidateInput(fmt.Sprintf("source-%03d", i), vector)
	}
	got, err := candidates.Find(context.Background(), anchor, others, nil)
	if err != nil || len(got) != candidates.MaxResults {
		t.Fatalf("maximum bounded input failed: %d %v", len(got), err)
	}
	for _, item := range got {
		if item.Reasons[0].Fingerprint.OffsetItems != 0 || item.Reasons[0].Fingerprint.ComparedItems != 1000 {
			t.Fatal("equal fingerprint lost full overlap")
		}
	}
}
