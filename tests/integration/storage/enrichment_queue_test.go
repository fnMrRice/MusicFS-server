package storage_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
)

func enrichmentRequest(ids ...string) enrichment.JobRequest {
	return enrichment.JobRequest{SourceTrackIDs: ids, Fields: []string{"lyrics", "artwork"}, Languages: []string{}, InstanceIDs: []string{"scraper"}, ExcludeInstanceIDs: []string{}}
}

func TestEnrichmentQueueScopeIdempotencyAndRestartGeneration(t *testing.T) {
	s, _, tracks := matchingFixture(t, 2)
	ctx := context.Background()
	request := enrichmentRequest(tracks[0].ID, tracks[1].ID)
	j, replay, err := s.QueueEnrichment(ctx, "admin", request, "metadata-job-key1")
	if err != nil || replay || len(j.Targets) != 2 || j.State != "queued" {
		t.Fatal("queue metadata", err)
	}
	again, replay, err := s.QueueEnrichment(ctx, "admin", request, "metadata-job-key1")
	if err != nil || !replay || !reflect.DeepEqual(j, again) {
		t.Fatal("queue replay changed", err)
	}
	request.Fields = []string{"lyrics"}
	_, _, err = s.QueueEnrichment(ctx, "admin", request, "metadata-job-key1")
	matchingError(t, err, "CONFLICT")
	if _, err = s.GetEnrichmentJob(ctx, "bob", j.JobID); err == nil {
		t.Fatal("cross-owner job exposed")
	}
	if _, err = s.CancelEnrichment(ctx, "bob", j.JobID); err == nil {
		t.Fatal("cross-owner job cancelled")
	}
	private := matchingPrivateTrack(t, s)
	_, _, err = s.QueueEnrichment(ctx, "admin", enrichmentRequest(tracks[0].ID, private.ID), domain.NewID())
	matchingError(t, err, "NOT_FOUND")
	w, err := s.ClaimEnrichment(ctx)
	if err != nil {
		t.Fatal(err)
	}
	forged := w
	forged.Request.Fields = []string{"artwork"}
	if err = s.CurrentEnrichment(ctx, forged); err == nil {
		t.Fatal("execution ticket changed persisted request")
	}
	if err = s.ReserveEnrichmentPreview(ctx, w, 100); err != nil {
		t.Fatal(err)
	}
	var number int
	var name, path string
	if err = s.DB.QueryRow("PRAGMA database_list").Scan(&number, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.RecoverEnrichment(ctx); err != nil {
		t.Fatal(err)
	}
	if err = reopened.CurrentEnrichment(ctx, w); err == nil {
		t.Fatal("old generation survived restart")
	}
	newWork, err := reopened.ClaimEnrichment(ctx)
	if err != nil || newWork.Target != w.Target || newWork.Generation != w.Generation+1 {
		t.Fatal("recovered target changed", err)
	}
	if err = reopened.FinishEnrichment(ctx, w, enrichment.Outcome{}); err == nil {
		t.Fatal("old callback published")
	}
	if _, err = reopened.CancelEnrichment(ctx, "admin", j.JobID); err != nil {
		t.Fatal(err)
	}
	if err = reopened.ReserveEnrichmentPreview(ctx, newWork, 1); err == nil {
		t.Fatal("cancelled target reserved preview")
	}
	if err = reopened.FinishEnrichment(ctx, newWork, enrichment.Outcome{}); err == nil {
		t.Fatal("cancelled target finished")
	}
	final, err := reopened.CancelEnrichment(ctx, "admin", j.JobID)
	if err != nil || final.State != "cancelled" {
		t.Fatal("cancel replay failed", err)
	}
}

func TestEnrichmentBatchBudgetsAndIndependentTargetOutcomes(t *testing.T) {
	s, _, tracks := matchingFixture(t, 2)
	ctx := context.Background()
	j, _, err := s.QueueEnrichment(ctx, "admin", enrichmentRequest(tracks[0].ID, tracks[1].ID), domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.ClaimEnrichment(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if err = s.ReserveEnrichmentPreview(ctx, w, 8<<20); err != nil {
			t.Fatal(err)
		}
	}
	matchingError(t, s.ReserveEnrichmentPreview(ctx, w, 1), "RATE_LIMITED")
	code := "UNSUPPORTED"
	if err = s.FinishEnrichment(ctx, w, enrichment.Outcome{FailureCode: &code}); err != nil {
		t.Fatal(err)
	}
	next, err := s.ClaimEnrichment(ctx)
	if err != nil {
		t.Fatal(err)
	}
	matchingError(t, s.ReserveEnrichmentPreview(ctx, next, 1), "RATE_LIMITED")
	for i := 0; i < enrichment.MaxCandidates; i++ {
		if err = s.ReserveEnrichmentPreview(ctx, next, 0); err != nil {
			t.Fatal(err)
		}
	}
	matchingError(t, s.ReserveEnrichmentPreview(ctx, next, 0), "RATE_LIMITED")
	if err = s.FinishEnrichment(ctx, next, enrichment.Outcome{}); err != nil {
		t.Fatal(err)
	}
	final, err := s.GetEnrichmentJob(ctx, "admin", j.JobID)
	if err != nil || final.State != "completed" || final.Targets[0].State == final.Targets[1].State {
		t.Fatal("partial results lost", err)
	}
}
