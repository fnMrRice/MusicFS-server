package identification_test

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/matching/standard"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/candidates"
)

func TestCandidatesVerifiedFileHashPreservesSeparateSources(t *testing.T) {
	a, b := candidates.Input{Asset: candidateAsset("a")}, candidates.Input{Asset: candidateAsset("b")}
	a.FileHash = &candidates.VerifiedHash{Asset: a.Asset, EvidenceID: "hash-a", SHA512: strings.Repeat("c", 128)}
	b.FileHash = &candidates.VerifiedHash{Asset: b.Asset, EvidenceID: "hash-b", SHA512: a.FileHash.SHA512}
	a.Title, b.Title = "Original title", "Changed title (Live)"
	got, err := standard.Compare(context.Background(), a, b)
	if err != nil || got == nil || got.Score != 100 || !got.RequiresConfirmation || got.AlgorithmVersion != candidates.AlgorithmVersion || !candidateHasReason(got, "verifiedFileSHA512Equal") || got.Left == got.Right {
		t.Fatalf("hash candidate missing separate current source bindings: %+v %v", got, err)
	}
	reversed, err := standard.Compare(context.Background(), b, a)
	if err != nil || !reflect.DeepEqual(got, reversed) {
		t.Fatal("candidate orientation is not deterministic")
	}
	b.FileHash.FileSizeBytes = 1
	if got, err = standard.Compare(context.Background(), a, b); err != nil || got != nil {
		t.Fatal("equal hashes with contradictory byte lengths became complete-file evidence")
	}
	b.FileHash.FileSizeBytes = 0
	b.Asset.AssetVersion = "version-2"
	if got, err = standard.Compare(context.Background(), a, b); err != nil || got != nil {
		t.Fatal("old asset hash proved the new asset")
	}
	b.Asset = b.FileHash.Asset
	b.FileHash.SHA512 = strings.Repeat("C", 128)
	if _, err = standard.Compare(context.Background(), a, b); err == nil {
		t.Fatal("malformed verified hash accepted")
	}
	if got, err = standard.Compare(context.Background(), a, a); err != nil || got != nil {
		t.Fatal("source matched itself")
	}
}

func TestCandidatesMetadataIsWeakAndKeepsVersionQualifiers(t *testing.T) {
	d1, d2 := int64(100000), int64(102000)
	a := candidates.Input{Asset: candidateAsset("a"), Title: "  A\u2003Song (Live) ", Artists: []string{"", " Artist "}, DurationMs: &d1}
	b := candidates.Input{Asset: candidateAsset("b"), Title: "a song (live)", Artists: []string{"Guest", "ARTIST"}, DurationMs: &d2}
	got, err := standard.Compare(context.Background(), a, b)
	if err != nil || got == nil || got.Score > 40 || !got.RequiresConfirmation || !candidateHasReason(got, "metadataOnly") {
		t.Fatalf("weak metadata evidence missing: %+v %v", got, err)
	}
	for _, mutate := range []func(*candidates.Input){
		func(v *candidates.Input) { v.Title = "a song" },
		func(v *candidates.Input) { v.Title = "a song (remix)" },
		func(v *candidates.Input) { v.Artists = []string{"", " "} },
		func(v *candidates.Input) { v.DurationMs = nil },
		func(v *candidates.Input) { duration := int64(102001); v.DurationMs = &duration },
	} {
		copy := b
		mutate(&copy)
		if got, err = standard.Compare(context.Background(), a, copy); err != nil || got != nil {
			t.Fatal("incomplete or differently versioned tags became a candidate")
		}
	}
	a.FileHash = &candidates.VerifiedHash{Asset: a.Asset, EvidenceID: "hash-a", SHA512: strings.Repeat("a", 128)}
	b.FileHash = &candidates.VerifiedHash{Asset: b.Asset, EvidenceID: "hash-b", SHA512: strings.Repeat("b", 128)}
	got, err = standard.Compare(context.Background(), a, b)
	if err != nil || !candidateHasReason(got, "metadataOnly") {
		t.Fatal("different container bytes incorrectly proved different recordings")
	}
}

func TestCandidatesActualSyntheticFingerprintVariants(t *testing.T) {
	vectors := candidateFixtures(t)
	original := candidateInput("a", vectors["original.pcm"])
	for _, name := range []string{"lossless.pcm", "mp3-64.pcm", "mp3-192.pcm", "aac-128.pcm", "silence-prefix.pcm", "alternate-take.pcm"} {
		t.Run(name, func(t *testing.T) {
			other := candidateInput("b", vectors[name])
			got, err := standard.Compare(context.Background(), original, other)
			if err != nil || got == nil || got.Score < 70 || got.Score >= 100 || !got.RequiresConfirmation || !candidateHasReason(got, "fingerprintSimilar") {
				t.Fatalf("expected review candidate: %+v %v", got, err)
			}
			metric := got.Reasons[0].Fingerprint
			if metric == nil || metric.BER > .08 || metric.ComparedItems < 50 {
				t.Fatal("missing bounded comparison metrics")
			}
			if name == "silence-prefix.pcm" && metric.OffsetItems != 24 {
				t.Fatal("leading silence search did not retain its bounded index offset")
			}
			if name == "alternate-take.pcm" && metric.BER < .04 {
				t.Fatal("different synthetic performance counterexample changed")
			}
			reversed, err := standard.Compare(context.Background(), other, original)
			if err != nil || !reflect.DeepEqual(got, reversed) {
				t.Fatal("fingerprint pair orientation changed result")
			}
		})
	}
	for _, name := range []string{"live-style.pcm", "remix.pcm", "unrelated.pcm"} {
		other := candidateInput("b", vectors[name])
		a := original
		a.Title = "Same Song"
		a.Artists = []string{"Same Artist"}
		other.Title = a.Title
		other.Artists = a.Artists
		if got, err := standard.Compare(context.Background(), a, other); err != nil || got != nil {
			t.Fatalf("incompatible informative fingerprint fell back to tags: %s %+v %v", name, got, err)
		}
	}
	partial := candidateInput("b", vectors["partial-10-30.pcm"])
	if got, err := standard.Compare(context.Background(), original, partial); err != nil || got != nil {
		t.Fatal("different requested coverage was guessed into an alignment")
	}
	if !slices.Equal(vectors["tone-440.pcm"].Values, vectors["tone-880.pcm"].Values) {
		t.Fatal("actual pure-tone collision fixture changed")
	}
	for _, pair := range [][2]string{{"tone-440.pcm", "tone-880.pcm"}, {"silence.pcm", "tone-440.pcm"}} {
		if got, err := standard.Compare(context.Background(), candidateInput("a", vectors[pair[0]]), candidateInput("b", vectors[pair[1]])); err != nil || got != nil {
			t.Fatal("low-information collision became fingerprint evidence")
		}
	}
}
