package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/identification/fingerprint"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
)

func TestFingerprintPublicationReferencesOnlyDurableValidatedFile(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	track := publishEvidenceTracks(t, s, evidenceObservation("track", "v1"))[0]
	j := fpClaim(t, s, fpTarget(track))
	p, raw := fpPublication(t, j)
	files := fpFiles(t, t.TempDir())
	service, err := mediastore.New(s, files)
	if err != nil {
		t.Fatal(err)
	}
	guarded := false
	final, err := service.Publish(ctx, p.Owner, p.Kind, p.Object.MIME, p.Target, bytes.NewReader(raw), fingerprint.MaxDetailBytes, fpValidator, func(publish func() error) error { guarded = true; return publish() })
	if err != nil || !guarded || final.Status != "finished" {
		t.Fatal("guarded publication failed", err)
	}
	done, err := s.GetFingerprintJob(ctx, "admin", j.ID)
	if err != nil || done.Status != "succeeded" || done.Result == nil {
		t.Fatal("job and index not committed together", err)
	}
	i := done.Result
	if !i.Current || i.FingerprintTarget != j.FingerprintTarget || i.SampleCount <= 0 || i.ValueCount != 20 || i.ComputedAt.Location().String() != "UTC" {
		t.Fatalf("invalid fingerprint index: %+v", i)
	}
	read, err := files.Open(i.Object)
	if err != nil {
		t.Fatal(err)
	}
	content, err := io.ReadAll(read)
	closeErr := read.Close()
	if err != nil || closeErr != nil || !bytes.Equal(raw, content) {
		t.Fatal("stored fingerprint bytes differ", err, closeErr)
	}
	for _, table := range []string{"fingerprint_jobs", "fingerprint_indexes", "media_publications"} {
		var document []byte
		if err = s.DB.QueryRowContext(ctx, "SELECT document FROM "+table+" LIMIT 1").Scan(&document); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(document, []byte(`"values":`)) {
			t.Fatal("raw fingerprint array was persisted in database", table)
		}
	}
	public, err := json.Marshal(done)
	if err != nil || bytes.Contains(public, []byte("RelativePath")) || bytes.Contains(public, []byte("objects/")) {
		t.Fatal("private media location leaked", err)
	}
	if _, err = s.FingerprintIndex(ctx, "other", i.ID); err == nil {
		t.Fatal("private fingerprint index disclosed")
	}
	if _, err = s.FingerprintIndexes(ctx, "other", track.ID, "", 100); err == nil {
		t.Fatal("private track index listing disclosed")
	}
	listed, err := s.FingerprintIndexes(ctx, "admin", track.ID, "", 100)
	if err != nil || len(listed) != 1 || listed[0].ID != i.ID {
		t.Fatal("current index missing", err)
	}
	if err = s.ReferencePublication(ctx, "admin", final.ID); err != nil {
		t.Fatal("reference retry failed", err)
	}
	again, err := s.GetFingerprintJob(ctx, "admin", j.ID)
	if err != nil || !reflect.DeepEqual(done, again) {
		t.Fatal("successful reference retry mutated result", err)
	}
	reuse, _, err := s.QueueFingerprint(ctx, "admin", j.FingerprintTarget, domain.NewID(), false)
	if err != nil || reuse.Status != "succeeded" || reuse.Result == nil || reuse.Result.ID != i.ID {
		t.Fatal("same exact extractor/range did not reuse valid index", err)
	}
	if err = s.InvalidateFingerprint(ctx, "other", i.ID); err == nil {
		t.Fatal("another owner invalidated a private fingerprint")
	}
	if err = s.InvalidateFingerprint(ctx, "admin", i.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{j.ID, reuse.ID} {
		lost, err := s.GetFingerprintJob(ctx, "admin", id)
		if err != nil || lost.Status != "unavailable" || lost.Result == nil || lost.Result.Current || lost.Error.Code != "RESOURCE_CHANGED" {
			t.Fatal("lost file still claimed usable", err)
		}
	}
	newJob, _, err := s.QueueFingerprint(ctx, "admin", j.FingerprintTarget, domain.NewID(), false)
	if err != nil || newJob.Status != "queued" {
		t.Fatal("unavailable result prevented recomputation", err)
	}
}

func TestFingerprintPublicationRejectsCancelledStaleAndForgedTargets(t *testing.T) {
	for _, scenario := range []string{"cancelled", "asset-changed", "old-generation", "wrong-owner", "wrong-summary", "phase-skip", "object-replacement"} {
		t.Run(scenario, func(t *testing.T) {
			s := evidenceStore(t)
			ctx := context.Background()
			track := publishEvidenceTracks(t, s, evidenceObservation("track", "v1"))[0]
			j := fpClaim(t, s, fpTarget(track))
			p, raw := fpPublication(t, j)
			files := fpFiles(t, t.TempDir())
			if scenario == "wrong-summary" {
				p.Target.Metadata = bytes.Replace(p.Target.Metadata, []byte(`"valueCount":20`), []byte(`"valueCount":0`), 1)
				if err := s.BeginPublication(ctx, p); err == nil {
					t.Fatal("inconsistent fingerprint summary accepted")
				}
				return
			}
			p = fpAdvance(t, s, files, p, raw, "published")
			switch scenario {
			case "cancelled":
				if _, err := s.CancelFingerprint(ctx, "admin", j.ID); err != nil {
					t.Fatal(err)
				}
			case "asset-changed":
				publishEvidenceTracks(t, s, evidenceObservation("track", "v2"))
			case "old-generation":
				if err := s.RecoverFingerprints(ctx); err != nil {
					t.Fatal(err)
				}
				if _, err := s.ClaimFingerprint(ctx); err != nil {
					t.Fatal(err)
				}
			case "wrong-owner":
				if err := s.ReferencePublication(ctx, "other", p.ID); err == nil {
					t.Fatal("another owner referenced publication")
				}
				return
			case "phase-skip":
				if err := s.SetPublicationStage(ctx, "admin", p.ID, "published", "referenced"); err == nil {
					t.Fatal("phase transition bypassed reference transaction")
				}
				return
			case "object-replacement":
				o := p.Object
				o.SHA256 = strings.Repeat("0", 64)
				if err := s.WrittenPublication(ctx, "admin", p.ID, o); err == nil {
					t.Fatal("published object was replaced")
				}
				return
			}
			if err := s.ReferencePublication(ctx, "admin", p.ID); err == nil {
				t.Fatal("stale or cancelled extraction was published")
			}
			var count int
			if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM fingerprint_indexes").Scan(&count); err != nil || count != 0 {
				t.Fatal("rejected publication created an index", err)
			}
		})
	}
}

func TestFingerprintAssetReplacementPreservesAuditButRemovesCurrentIndex(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	track := publishEvidenceTracks(t, s, evidenceObservation("track", "v1"))[0]
	j := fpClaim(t, s, fpTarget(track))
	p, raw := fpPublication(t, j)
	files := fpFiles(t, t.TempDir())
	p = fpAdvance(t, s, files, p, raw, "referenced")
	before, err := s.GetFingerprintJob(ctx, "admin", j.ID)
	if err != nil || before.Result == nil {
		t.Fatal(err)
	}
	publishEvidenceTracks(t, s, evidenceObservation("track", "v2"))
	index, err := s.FingerprintIndex(ctx, "admin", before.Result.ID)
	if err != nil || index.Current || index.AssetID != track.AssetID {
		t.Fatal("invalidated fingerprint audit lost or still current", err)
	}
	job, err := s.GetFingerprintJob(ctx, "admin", j.ID)
	if err != nil || job.Result == nil || job.Result.Current {
		t.Fatal("job exposed stale current flag", err)
	}
	current, err := s.FingerprintIndexes(ctx, "admin", track.ID, "", 100)
	if err != nil || len(current) != 0 {
		t.Fatal("old asset fingerprint entered current listing", err)
	}
	if err = s.SetPublicationStage(ctx, "admin", p.ID, "referenced", "finished"); err != nil {
		t.Fatal(err)
	}
}

func TestFingerprintCancellationAndReferenceAreAtomic(t *testing.T) {
	for range 8 {
		s := evidenceStore(t)
		ctx := context.Background()
		track := publishEvidenceTracks(t, s, evidenceObservation("track", "v1"))[0]
		j := fpClaim(t, s, fpTarget(track))
		p, raw := fpPublication(t, j)
		p = fpAdvance(t, s, fpFiles(t, t.TempDir()), p, raw, "published")
		start := make(chan struct{})
		var wg sync.WaitGroup
		var referenceErr, cancelErr error
		wg.Go(func() { <-start; referenceErr = s.ReferencePublication(ctx, "admin", p.ID) })
		wg.Go(func() { <-start; _, cancelErr = s.CancelFingerprint(ctx, "admin", j.ID) })
		close(start)
		wg.Wait()
		if cancelErr != nil {
			t.Fatal(cancelErr)
		}
		result, err := s.GetFingerprintJob(ctx, "admin", j.ID)
		if err != nil {
			t.Fatal(err)
		}
		var indexes int
		if err = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM fingerprint_indexes").Scan(&indexes); err != nil {
			t.Fatal(err)
		}
		if result.Status == "succeeded" {
			if referenceErr != nil || indexes != 1 || result.Result == nil {
				t.Fatal("reference did not commit atomically", referenceErr)
			}
		} else if result.Status == "cancelled" {
			if referenceErr == nil || indexes != 0 || result.Result != nil {
				t.Fatal("cancelled job leaked an index")
			}
		} else {
			t.Fatal("unexpected terminal state", result.Status)
		}
	}
}

func TestFingerprintSummaryRejectsFalseCoverageAndQuality(t *testing.T) {
	s := evidenceStore(t)
	target := fpTarget(publishEvidenceTracks(t, s, evidenceObservation("track", "v1"))[0])
	j := fpClaim(t, s, target)
	p, _ := fpPublication(t, j)
	var valid identification.FingerprintSummary
	if err := json.Unmarshal(p.Target.Metadata, &valid); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*identification.FingerprintSummary){
		func(s *identification.FingerprintSummary) { s.ActualStartSample++ }, func(s *identification.FingerprintSummary) { s.SampleCount = 330751 },
		func(s *identification.FingerprintSummary) { s.ExtractorIdentity = fpIdentity("other") }, func(s *identification.FingerprintSummary) { s.ValueCount = 1001 },
		func(s *identification.FingerprintSummary) { s.Quality.Status = "short" }, func(s *identification.FingerprintSummary) { s.Quality.UniqueValues = 0 },
	} {
		invalid := valid
		mutate(&invalid)
		if err := invalid.Validate(target); err == nil {
			t.Fatal("invalid fingerprint coverage or quality accepted")
		}
	}
	short := valid
	short.SampleCount = 11024
	short.ValueCount = 0
	short.Quality = fingerprint.Quality{Status: "short"}
	if err := short.Validate(target); err != nil {
		t.Fatal("real short tail cannot be recorded", err)
	}
	low := valid
	low.ValueCount = 20
	low.Quality = fingerprint.Quality{Status: "lowInformation", UniqueValues: 1, DominantValues: 20}
	if err := low.Validate(target); err != nil {
		t.Fatal("low information cannot be recorded", err)
	}
}
