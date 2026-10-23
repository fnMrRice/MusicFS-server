package storage_test

import (
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func queuedRefreshTask(t *testing.T, s *sqlite.Store, owner, provider string) jobs.Job {
	t.Helper()
	plan := testkit.ScanPlan("music")
	plan.RefreshMetadata = true
	j, _, err := s.Enqueue(t.Context(), owner, provider, plan, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func runningRefreshTask(t *testing.T, s *sqlite.Store) jobs.Job {
	t.Helper()
	queuedRefreshTask(t, s, "admin", "local")
	j, err := s.Claim(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func metadataRefreshTask(t *testing.T, s *sqlite.Store) (jobs.Job, enrichment.AutomationWork, enrichment.Work, library.Track) {
	t.Helper()
	ctx := t.Context()
	j := runningRefreshTask(t, s)
	observation := evidenceObservation("refresh-task", "v1")
	observation.RootID = "music"
	if err := s.Publish(ctx, j, testkit.ScanPublication(j, []library.Observation{observation})); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(ctx, j, 1, nil); err != nil {
		t.Fatal(err)
	}
	w, err := s.NextAutomation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	policy := enrichment.AutomaticPolicy{Enabled: true, InstancePriority: []string{"scraper"}, Fields: []string{"lyrics"}}
	if err = s.PrepareAutomation(ctx, w.TriggerID, policy); err != nil {
		t.Fatal(err)
	}
	job, replay, err := s.QueueAutomaticEnrichment(ctx, "admin", w.TriggerID, enrichmentRequest(w.SourceTrackID))
	if err != nil || replay {
		t.Fatal("atomic automatic queue failed", job, replay, err)
	}
	w, err = s.NextAutomation(ctx)
	if err != nil || w.JobID == nil || *w.JobID != job.JobID {
		t.Fatal("automatic job was not atomically attached", w, err)
	}
	work, err := s.ClaimEnrichment(ctx)
	if err != nil {
		t.Fatal(err)
	}
	track, err := s.FindTrack(ctx, "admin", w.SourceTrackID)
	if err != nil {
		t.Fatal(err)
	}
	return j, w, work, track
}

func finishRefreshPreview(t *testing.T, s *sqlite.Store, w enrichment.Work, track library.Track) enrichment.PreviewCandidate {
	t.Helper()
	id := domain.NewID()
	input := contentInput(track, "und", "Refresh task preview")
	input.Metadata.Origin = mediastore.Origin{Kind: "scraper", InstanceID: "scraper", CandidateID: id}
	object := contentPublish(t, s, "admin", input)
	c := enrichment.PreviewCandidate{CandidateID: id, Target: w.Target, InstanceID: "scraper", Field: "lyrics", Language: "und", Format: "text", MIME: object.MIME, SizeBytes: object.Size, SHA256: object.SHA256, Origin: "sidecar", Basis: "exactStem", Status: "available", PreviewObjectID: &object.ID, ExpiresAt: time.Now().UTC().Add(enrichment.CandidateLifetime)}
	if err := s.ReserveEnrichmentPreview(t.Context(), w, c.SizeBytes); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishEnrichment(t.Context(), w, enrichment.Outcome{Candidates: []enrichment.PreviewCandidate{c}}); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRefreshTaskListScopePaginationAndQueuedCancellation(t *testing.T) {
	s := evidenceStore(t)
	ctx := t.Context()
	ordinary, _, err := s.Enqueue(ctx, "admin", "ordinary", testkit.ScanPlan("music"), domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	first := queuedRefreshTask(t, s, "admin", "first")
	second := queuedRefreshTask(t, s, "admin", "second")
	foreign := queuedRefreshTask(t, s, "other", "third")
	page, err := s.ListRefreshTasks(ctx, "admin", "", 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != second.ID || page.ActiveCount != 2 || page.NextCursor == nil {
		t.Fatal("task page is not scoped or counted", page, err)
	}
	tail, err := s.ListRefreshTasks(ctx, "admin", *page.NextCursor, 200)
	if err != nil || len(tail.Items) != 1 || tail.Items[0].ID != first.ID || tail.ActiveCount != 2 {
		t.Fatal(tail, err)
	}
	all, err := s.ListRefreshTasks(ctx, "", "", 200)
	if err != nil || len(all.Items) != 3 || all.ActiveCount != 3 {
		t.Fatal(all, err)
	}
	for _, id := range []string{foreign.ID, ordinary.ID} {
		_, err = s.GetRefreshTask(ctx, "admin", id)
		matchingError(t, err, "NOT_FOUND")
		_, err = s.CancelRefreshTask(ctx, "admin", id)
		matchingError(t, err, "NOT_FOUND")
	}
	_, err = s.ListRefreshTasks(ctx, "admin", foreign.ID, 10)
	matchingError(t, err, "INVALID_ARGUMENT")
	cancelled, err := s.CancelRefreshTask(ctx, "admin", first.ID)
	if err != nil || cancelled.Status != "cancelled" || !cancelled.CancelRequested || cancelled.Cancellable {
		t.Fatal(cancelled, err)
	}
	again, err := s.CancelRefreshTask(ctx, "admin", first.ID)
	if err != nil || !reflect.DeepEqual(cancelled, again) {
		t.Fatal("queued cancellation was not idempotent", again, err)
	}
	page, err = s.ListRefreshTasks(ctx, "admin", "", 200)
	if err != nil || page.ActiveCount != 1 {
		t.Fatal(page, err)
	}
}

func TestRefreshTaskScanCancellationFencesPublicationAndSurvivesRecovery(t *testing.T) {
	s := evidenceStore(t)
	ctx := t.Context()
	j := runningRefreshTask(t, s)
	state, err := s.CancelRefreshTask(ctx, "admin", j.ID)
	if err != nil || state.Status != "cancelling" {
		t.Fatal(state, err)
	}
	observation := evidenceObservation("late", "v1")
	observation.RootID = "music"
	err = s.Publish(ctx, j, testkit.ScanPublication(j, []library.Observation{observation}))
	matchingError(t, err, "CONFLICT")
	err = s.Finish(ctx, j, 1, nil)
	matchingError(t, err, "CONFLICT")
	if _, err = s.Claim(ctx); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cancelled scan was claimed", err)
	}
	page, err := s.ListRefreshTasks(ctx, "admin", "", 200)
	if err != nil || page.ActiveCount != 1 {
		t.Fatal("cancelling task not counted", page, err)
	}
	if err = s.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	state, err = s.GetRefreshTask(ctx, "admin", j.ID)
	if err != nil || state.Status != "cancelled" {
		t.Fatal("restart did not finish cancellation", state, err)
	}
	if err = s.CompleteRefreshTaskCancellation(ctx, "admin", j.ID); err != nil {
		t.Fatal(err)
	}
	page, err = s.ListRefreshTasks(ctx, "admin", "", 200)
	if err != nil || page.ActiveCount != 0 {
		t.Fatal(page, err)
	}
}

func TestRefreshTaskCancellationFencesMetadataQueueAndLateCompletion(t *testing.T) {
	s := evidenceStore(t)
	ctx := t.Context()
	j, automation, work, _ := metadataRefreshTask(t, s)
	state, err := s.GetRefreshTask(ctx, "admin", j.ID)
	if err != nil || state.Status != "running" || state.Phase != "metadata" || state.Progress.MetadataTotal != 1 {
		t.Fatal(state, err)
	}
	state, err = s.CancelRefreshTask(ctx, "admin", j.ID)
	if err != nil || state.Status != "cancelling" {
		t.Fatal(state, err)
	}
	ids, err := s.RefreshTaskMetadataJobs(ctx, "admin", j.ID)
	if err != nil || len(ids) != 1 || ids[0] != work.JobID {
		t.Fatal(ids, err)
	}
	_, _, err = s.QueueAutomaticEnrichment(ctx, "admin", automation.TriggerID, enrichmentRequest(work.Target.SourceTrackID))
	matchingError(t, err, "CANCELED")
	err = s.AttachAutomationJob(ctx, automation.TriggerID, work.JobID)
	matchingError(t, err, "CANCELED")
	err = s.FinishEnrichment(ctx, work, enrichment.Outcome{})
	matchingError(t, err, "CONFLICT")
	if err = s.RecoverEnrichment(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ClaimEnrichment(ctx); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cancelled metadata was reclaimed", err)
	}
	if err = s.CompleteRefreshTaskCancellation(ctx, "admin", j.ID); err != nil {
		t.Fatal(err)
	}
	state, err = s.GetRefreshTask(ctx, "admin", j.ID)
	if err != nil || state.Status != "cancelled" {
		t.Fatal(state, err)
	}
}

func TestRefreshTaskCancellationRejectsNewApplyButPreservesCommittedResult(t *testing.T) {
	for _, committed := range []bool{false, true} {
		name := "new-apply"
		if committed {
			name = "successful-replay"
		}
		t.Run(name, func(t *testing.T) {
			s := evidenceStore(t)
			ctx := t.Context()
			j, automation, work, track := metadataRefreshTask(t, s)
			c := finishRefreshPreview(t, s, work, track)
			a := enrichment.AutomationAction{Field: "lyrics", Language: "und", Status: "pending", CandidateID: &c.CandidateID}
			if _, err := s.AutomationAction(ctx, automation.TriggerID, a); err != nil {
				t.Fatal(err)
			}
			guard := func(publish func() error) error { return publish() }
			key := domain.NewID()
			if committed {
				if _, err := s.ApplyAutomaticEnrichment(ctx, "admin", c.CandidateID, *c.PreviewObjectID, 0, key, true, guard); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.CancelRefreshTask(ctx, "admin", j.ID); err != nil {
				t.Fatal(err)
			}
			called := false
			selected, err := s.ApplyAutomaticEnrichment(ctx, "admin", c.CandidateID, *c.PreviewObjectID, 0, key, true, func(publish func() error) error { called = true; return publish() })
			if committed {
				if err != nil || called || selected.Revision != 1 {
					t.Fatal("committed apply did not replay", selected, err)
				}
				a.Status, a.Reason = "applied", "selected"
				if err = s.FinishAutomationAction(ctx, automation.TriggerID, a); err != nil {
					t.Fatal(err)
				}
			} else {
				matchingError(t, err, "CANCELED")
				if called {
					t.Fatal("cancelled apply entered guard")
				}
			}
			if err = s.CompleteRefreshTaskCancellation(ctx, "admin", j.ID); err != nil {
				t.Fatal(err)
			}
			state, err := s.GetRefreshTask(ctx, "admin", j.ID)
			if err != nil || state.Status != "cancelled" || committed && state.Result.Updated != 1 {
				t.Fatal(state, err)
			}
			before := state.Result
			a.Status = "failed"
			if err = s.FinishAutomationAction(ctx, automation.TriggerID, a); err != nil {
				t.Fatal(err)
			}
			state, err = s.GetRefreshTask(ctx, "admin", j.ID)
			if err != nil || state.Result != before {
				t.Fatal("late action changed finished cancellation", state, err)
			}
		})
	}
}

func TestRefreshTaskSummaryKeepsFinishedResultsAndCountsActiveMetadata(t *testing.T) {
	s := evidenceStore(t)
	ctx := t.Context()
	j, automation, _, _ := metadataRefreshTask(t, s)
	for _, action := range []enrichment.AutomationAction{
		{Field: "artwork", Language: "", Status: "applied", Reason: "selected"},
		{Field: "lyrics", Language: "en", Status: "skipped", Reason: "manualLocked"},
		{Field: "lyrics", Language: "zh", Status: "skipped", Reason: "noEligibleCandidate"},
	} {
		if _, err := s.AutomationAction(ctx, automation.TriggerID, action); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ListRefreshTasks(ctx, "admin", "", 200)
	if err != nil || page.ActiveCount != 1 || page.Items[0].Phase != "metadata" {
		t.Fatal(page, err)
	}
	if err = s.FinishAutomation(ctx, automation.TriggerID, "completed", nil); err != nil {
		t.Fatal(err)
	}
	before, err := s.GetRefreshTask(ctx, "admin", j.ID)
	if err != nil || before.Status != "succeeded" || before.Cancellable || before.Result.Updated != 1 || before.Result.Retained != 1 || before.Result.NotFound != 1 || before.Result.Failed != 0 || before.Progress.MetadataCompleted != 1 {
		t.Fatal(before, err)
	}
	after, err := s.CancelRefreshTask(ctx, "admin", j.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("terminal cancellation changed history", after, err)
	}
	page, err = s.ListRefreshTasks(ctx, "admin", "", 200)
	if err != nil || page.ActiveCount != 0 {
		t.Fatal(page, err)
	}
}
