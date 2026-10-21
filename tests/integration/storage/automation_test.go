package storage_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestAutomationOutboxWaitsForSuccessfulScanAndRejectsRecoveredGeneration(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	p := testkit.ScanPlan("music")
	p.Options.ImportLyrics = true
	if _, _, err := s.Enqueue(ctx, "admin", "local", p, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	j, err := s.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	items := []library.Observation{evidenceObservation("one", "v1")}
	bad := testkit.ScanPublication(j, items)
	bad.Observations[0].RootID = "outside"
	if err = s.Publish(ctx, j, bad); err == nil {
		t.Fatal("out-of-scope publication accepted")
	}
	var count int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM metadata_automation").Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	items[0].RootID = "music"
	if err = s.Publish(ctx, j, testkit.ScanPublication(j, items)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.NextAutomation(ctx); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("unfinished scan consumed", err)
	}
	if err = s.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = s.NextAutomation(ctx); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("recovered publication consumed", err)
	}
	j = claimScanWhenDue(t, s)
	if err = s.Publish(ctx, j, testkit.ScanPublication(j, items)); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(ctx, j, 1, nil); err != nil {
		t.Fatal(err)
	}
	w, err := s.NextAutomation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	policy := enrichment.AutomaticPolicy{Enabled: true, InstancePriority: []string{"z", "a"}, Fields: []string{"lyrics"}}
	if err = s.PrepareAutomation(ctx, w.TriggerID, policy); err != nil {
		t.Fatal(err)
	}
	w, err = s.NextAutomation(ctx)
	if err != nil || w.Policy == nil || w.Policy.InstancePriority[0] != "z" {
		t.Fatal("policy not durable", w, err)
	}
	other := policy
	other.InstancePriority = []string{"a"}
	if err = s.PrepareAutomation(ctx, w.TriggerID, other); err != nil {
		t.Fatal(err)
	}
	w, err = s.NextAutomation(ctx)
	if err != nil || len(w.Policy.InstancePriority) != 2 {
		t.Fatal("policy reinterpreted", err)
	}
	page, err := s.ScanAutomationResults(ctx, "admin", j.ID, "", 1)
	if err != nil || len(page.Items) != 1 || page.NextCursor == nil {
		t.Fatal(page, err)
	}
	if _, err = s.ScanAutomationResults(ctx, "bob", j.ID, "", 10); err == nil {
		t.Fatal("private result exposed")
	}
	if _, err = s.ScanAutomationResults(ctx, "admin", j.ID, "foreign", 10); err == nil {
		t.Fatal("foreign cursor accepted")
	}
	a := enrichment.AutomationAction{Field: "lyrics", Language: "en", Status: "pending", ExpectedRevision: 7}
	if _, err = s.AutomationAction(ctx, w.TriggerID, a); err != nil {
		t.Fatal(err)
	}
	var sequence int
	var dbName, dbPath string
	if err = s.DB.QueryRow("PRAGMA database_list").Scan(&sequence, &dbName, &dbPath); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	restored, err := s.NextAutomation(ctx)
	if err != nil || restored.TriggerID != w.TriggerID || restored.Policy.InstancePriority[0] != "z" {
		t.Fatal("outbox policy did not survive database reopen", err)
	}
	a.ExpectedRevision = 42
	a.Status = "skipped"
	prior, err := s.AutomationAction(ctx, w.TriggerID, a)
	if err != nil || prior.ExpectedRevision != 7 || prior.Status != "pending" {
		t.Fatal("pending apply revision changed", prior, err)
	}
}

func TestAutomaticApplyPreservesLocksAndSuccessfulReplay(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	j, c := enrichmentPreview(t, s, tracks[0])
	guard := func(publish func() error) error { return publish() }
	first, err := s.ApplyAutomaticEnrichment(ctx, "admin", c.CandidateID, *c.PreviewObjectID, 0, "automatic-apply-test", guard)
	if err != nil || first.Revision != 1 || first.Bindings[0].ManualLocked {
		t.Fatal(first, err)
	}
	if _, err = s.ApplyEnrichment(ctx, "admin", c.CandidateID, *c.PreviewObjectID, 1, "manual-change-test", guard); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyAutomaticEnrichment(ctx, "admin", c.CandidateID, *c.PreviewObjectID, 2, "new-auto-apply-test", guard); err == nil {
		t.Fatal("manual lock replaced")
	}
	if _, err = s.CancelEnrichment(ctx, "admin", j.JobID); err != nil {
		t.Fatal(err)
	}
	called := false
	replay, err := s.ApplyAutomaticEnrichment(ctx, "admin", c.CandidateID, *c.PreviewObjectID, 0, "automatic-apply-test", func(publish func() error) error { called = true; return publish() })
	if err != nil || called || replay.Revision != 1 {
		t.Fatal("automatic success did not replay", err)
	}
	current, err := s.ContentSelection(ctx, "admin", tracks[0].ID)
	if err != nil || current.Revision != 2 || !current.Bindings[0].ManualLocked {
		t.Fatal("replay changed current selection", err)
	}
}

func TestCanonicalLyricsAliasSharesManualLockAndUnlock(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	english := contentPublish(t, s, "admin", contentInput(tracks[0], "ENG", "Manual English lyrics"))
	if english.Metadata.Language != "en" {
		t.Fatal("language was not canonical")
	}
	contentSelect(t, s, english, 0, false)
	next := contentPublish(t, s, "admin", contentInput(tracks[0], "en", "Automatic English lyrics"))
	_, err := s.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: next.Metadata.Target, ObjectID: next.ID, ExpectedRevision: 1, Automatic: true}, domain.NewID())
	matchingError(t, err, "CONFLICT")
	unlocked, err := s.UnlockContent(ctx, "admin", mediastore.UnlockRequest{SourceTrackID: tracks[0].ID, ExpectedRevision: 1, Field: "lyrics", Language: "eng"}, domain.NewID())
	if err != nil || unlocked.Bindings[0].ManualLocked || unlocked.Bindings[0].Language != "en" {
		t.Fatal("alias unlock addressed another slot", err)
	}
}
