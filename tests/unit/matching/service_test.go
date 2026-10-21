package matching_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/candidates"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprint"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

type repository struct {
	matching.Repository
	snapshot   matching.Snapshot
	saved      []candidates.Candidate
	publishErr error
}

func (r *repository) CandidateSnapshot(_ context.Context, owner, id string, _ []string, _ int) (matching.Snapshot, error) {
	if owner != r.snapshot.Owner || id != r.snapshot.Anchor.Input.Asset.SourceTrackID {
		return matching.Snapshot{}, domain.Fail("NOT_FOUND", "Source not found")
	}
	return r.snapshot, nil
}
func (r *repository) SaveCandidates(_ context.Context, _ string, _ matching.Snapshot, values []candidates.Candidate) (matching.CandidatePage, error) {
	if r.publishErr != nil {
		return matching.CandidatePage{}, r.publishErr
	}
	r.saved = values
	return matching.CandidatePage{Revision: r.snapshot.Revision}, nil
}

type details struct {
	value          fingerprint.Detail
	err            error
	calls          int
	start, release chan struct{}
}

func (d *details) Detail(ctx context.Context, owner, id string) (fingerprint.Detail, error) {
	if owner != "owner" {
		return fingerprint.Detail{}, domain.Fail("NOT_FOUND", "Fingerprint not found")
	}
	d.calls++
	if d.start != nil {
		close(d.start)
		d.start = nil
		select {
		case <-d.release:
		case <-ctx.Done():
			return fingerprint.Detail{}, ctx.Err()
		}
	}
	return d.value, d.err
}

func fixture(t *testing.T) (*repository, *details) {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/identification/candidate-fingerprints.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Name        string
		Samples     int64
		Fingerprint []uint32
	}
	if err = json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	var values []uint32
	var samples int64
	for _, v := range vectors {
		if v.Name == "lossless.pcm" {
			values, samples = v.Fingerprint, v.Samples
		}
	}
	id := fingerprint.Identity{Algorithm: "chromaprint", AlgorithmVersion: fingerprint.AlgorithmVersion, AlgorithmConfig: "cli-2-raw-v1", NormalizationRevision: audio.NormalizationRevision, QualityRevision: fingerprint.QualityRevision, Fpcalc: fingerprint.ToolIdentity{Version: "fpcalc version 1.6.1 (test identity)", SHA256: strings.Repeat("a", 64)}, Decoder: fingerprint.ToolIdentity{Version: "ffmpeg version test identity", SHA256: strings.Repeat("b", 64)}}
	b, _ := json.Marshal(id)
	id.ConfigRevision = fmt.Sprintf("sha256-%x", sha256.Sum256(b))
	d := fingerprint.Detail{SchemaVersion: 1, Identity: id, RequestedDurationMs: 120000, PCM: audio.PCMInfo{Format: audio.FingerprintPCM, NormalizationRevision: audio.NormalizationRevision, SampleCount: samples, DecoderVersion: id.Decoder.Version, DecoderSHA256: id.Decoder.SHA256}, Quality: fingerprint.Assess(values, samples), Values: values}
	source := func(name string) matching.CandidateSource {
		a := candidates.AssetRef{SourceTrackID: name, AssetID: "asset-" + name, AssetVersion: "v1"}
		i := identification.FingerprintIndex{ID: "fp-" + name, FingerprintTarget: identification.FingerprintTarget{HashTarget: identification.HashTarget{SourceTrackID: name, AssetID: a.AssetID, AssetVersion: a.AssetVersion}, FingerprintRequest: identification.FingerprintRequest{DurationMs: d.RequestedDurationMs, ExtractorIdentity: id}}, SampleCount: samples, ValueCount: len(values), Quality: d.Quality, Current: true}
		return matching.CandidateSource{Input: candidates.Input{Asset: a}, Fingerprints: []identification.FingerprintIndex{i}}
	}
	return &repository{snapshot: matching.Snapshot{Owner: "owner", Revision: 3, Anchor: source("a"), Sources: []matching.CandidateSource{source("b")}}}, &details{value: d}
}

func TestRefreshLoadsCompatibleAuthorizedEvidence(t *testing.T) {
	r, d := fixture(t)
	page, err := matching.New(r, d).Refresh(context.Background(), "owner", "a", nil)
	if err != nil || page.Revision != 3 || len(r.saved) != 1 || d.calls != 2 || !r.saved[0].RequiresConfirmation {
		t.Fatalf("refresh: %+v %v saved=%+v calls=%d", page, err, r.saved, d.calls)
	}
	if _, err = matching.New(r, d).Refresh(context.Background(), "other", "a", nil); err == nil {
		t.Fatal("foreign source exposed")
	}
}

func TestRefreshRejectsChangedDetailAndStalePublication(t *testing.T) {
	t.Run("changed detail", func(t *testing.T) {
		r, d := fixture(t)
		d.value.RequestedStartMs = 1
		_, err := matching.New(r, d).Refresh(context.Background(), "owner", "a", nil)
		if err == nil || r.saved != nil {
			t.Fatal("changed detail published")
		}
	})
	t.Run("decision raced", func(t *testing.T) {
		r, d := fixture(t)
		r.publishErr = domain.Fail("CONFLICT", "Decision changed")
		_, err := matching.New(r, d).Refresh(context.Background(), "owner", "a", nil)
		if !errors.Is(err, r.publishErr) || r.saved != nil {
			t.Fatal("stale publication accepted")
		}
	})
	t.Run("missing detail", func(t *testing.T) {
		r, d := fixture(t)
		d.err = domain.Fail("RESOURCE_CHANGED", "Missing file")
		_, err := matching.New(r, d).Refresh(context.Background(), "owner", "a", nil)
		if err != nil || len(r.saved) != 0 {
			t.Fatal("missing detail matched")
		}
	})
	t.Run("mismatched index", func(t *testing.T) {
		r, d := fixture(t)
		r.snapshot.Sources[0].Fingerprints[0].AssetVersion = "old"
		_, err := matching.New(r, d).Refresh(context.Background(), "owner", "a", nil)
		if err != nil || len(r.saved) != 0 || d.calls != 0 {
			t.Fatal("stale index loaded")
		}
	})
}

func TestRefreshAdmissionAndCancellation(t *testing.T) {
	r, d := fixture(t)
	d.start = make(chan struct{})
	d.release = make(chan struct{})
	started := d.start
	s := matching.New(r, d)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.Refresh(ctx, "owner", "a", nil); done <- err }()
	<-started
	_, err := s.Refresh(context.Background(), "owner", "a", nil)
	var de *domain.Error
	if !errors.As(err, &de) || de.Code != "RATE_LIMITED" {
		t.Fatal("unbounded concurrent refresh")
	}
	cancel()
	if err = <-done; !errors.Is(err, context.Canceled) || r.saved != nil {
		t.Fatalf("cancelled publication: %v", err)
	}
}
