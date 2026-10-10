package storage_test

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
)

func TestFingerprintQueueBindsAssetRangeAndExtractor(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	track := publishEvidenceTracks(t, s, evidenceObservation("track", "v1"))[0]
	target := fpTarget(track)
	j, merged, err := s.QueueFingerprint(ctx, "admin", target, "initial-fingerprint", true)
	if err != nil || merged || j.Result != nil {
		t.Fatal("queue failed", err)
	}
	same, merged, err := s.QueueFingerprint(ctx, "admin", target, "manual-fingerprint", false)
	if err != nil || !merged || same.ID != j.ID || same.Automatic {
		t.Fatal("manual duplicate not merged/promoted", err)
	}
	changed := target
	changed.StartMs++
	if _, _, err = s.QueueFingerprint(ctx, "admin", changed, "initial-fingerprint", false); err == nil {
		t.Fatal("changed range reused idempotency key")
	}
	rangeJob, merged, err := s.QueueFingerprint(ctx, "admin", changed, "range-fingerprint", false)
	if err != nil || merged || rangeJob.ID == j.ID {
		t.Fatal("different range merged", err)
	}
	changed = target
	changed.ExtractorIdentity = fpIdentity("v2")
	toolJob, merged, err := s.QueueFingerprint(ctx, "admin", changed, "tool-fingerprint", false)
	if err != nil || merged || toolJob.ID == j.ID {
		t.Fatal("different tool identity merged", err)
	}
	if _, _, err = s.QueueFingerprint(ctx, "other", target, "private-fingerprint", false); err == nil {
		t.Fatal("another owner queued private source")
	}
	if _, err = s.GetFingerprintJob(ctx, "other", j.ID); err == nil {
		t.Fatal("private job leaked")
	}
	claimed, err := s.ClaimFingerprint(ctx)
	if err != nil || claimed.ID != j.ID {
		t.Fatal("manual priority claim failed", err)
	}
	for _, mutate := range []func(*identification.FingerprintJob){func(j *identification.FingerprintJob) { j.FileSizeBytes++ }, func(j *identification.FingerprintJob) { j.StartMs++ }, func(j *identification.FingerprintJob) { j.Generation++ }, func(j *identification.FingerprintJob) { j.ExtractorIdentity = fpIdentity("forged") }} {
		forged := claimed
		mutate(&forged)
		if err := s.FailFingerprint(ctx, forged, "TEMPORARY_FAILURE"); err == nil {
			t.Fatal("forged execution ticket accepted")
		}
	}
	cancelled, err := s.CancelFingerprint(ctx, "admin", j.ID)
	if err != nil || cancelled.Status != "cancelled" {
		t.Fatal("cancel failed", err)
	}
	retry, err := s.CancelFingerprint(ctx, "admin", j.ID)
	if err != nil || !reflect.DeepEqual(cancelled, retry) {
		t.Fatal("cancel was not naturally idempotent", err)
	}
	if err = s.FailFingerprint(ctx, claimed, "TEMPORARY_FAILURE"); err == nil {
		t.Fatal("cancelled execution completed late")
	}
}

func TestFingerprintConcurrentQueueAndRecoveryRejectOldExecution(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	target := fpTarget(publishEvidenceTracks(t, s, evidenceObservation("track", "v1"))[0])
	var wg sync.WaitGroup
	results := make(chan identification.FingerprintJob, 8)
	failures := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			j, _, err := s.QueueFingerprint(ctx, "admin", target, domain.NewID(), false)
			if err != nil {
				failures <- err
			} else {
				results <- j
			}
		})
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	id := ""
	for j := range results {
		if id != "" && id != j.ID {
			t.Fatal("identical fingerprint tasks split")
		}
		id = j.ID
	}
	old, err := s.ClaimFingerprint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RecoverFingerprints(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := s.ClaimFingerprint(ctx)
	if err != nil || current.Generation <= old.Generation || current.FingerprintTarget != old.FingerprintTarget {
		t.Fatal("recovery lost target or reused generation", err)
	}
	if err = s.FailFingerprint(ctx, old, "TEMPORARY_FAILURE"); err == nil {
		t.Fatal("old generation finished")
	}
	if err = s.FailFingerprint(ctx, current, "UNSUPPORTED"); err != nil {
		t.Fatal(err)
	}
	unavailable, err := s.GetFingerprintJob(ctx, "admin", id)
	if err != nil || unavailable.Status != "unavailable" || unavailable.Result != nil {
		t.Fatal("unsupported target did not remain unavailable", err)
	}
	changed := fpTarget(publishEvidenceTracks(t, s, evidenceObservation("track", "v2"))[0])
	stale, _, err := s.QueueFingerprint(ctx, "admin", changed, domain.NewID(), false)
	if err != nil {
		t.Fatal(err)
	}
	publishEvidenceTracks(t, s, evidenceObservation("track", "v3"))
	if _, err = s.ClaimFingerprint(ctx); err == nil {
		t.Fatal("changed queued asset was claimed")
	}
	stale, err = s.GetFingerprintJob(ctx, "admin", stale.ID)
	if err != nil || stale.Status != "superseded" {
		t.Fatal("stale job not superseded", err)
	}
}
