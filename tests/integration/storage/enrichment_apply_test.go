package storage_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
)

func enrichmentPreview(t *testing.T, s *sqlite.Store, track library.Track) (enrichment.Job, enrichment.PreviewCandidate) {
	t.Helper()
	ctx := context.Background()
	j, _, err := s.QueueEnrichment(ctx, "admin", enrichmentRequest(track.ID), domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.ClaimEnrichment(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := domain.NewID()
	input := contentInput(track, "und", "Synthetic preview body")
	input.Metadata.Origin = mediastore.Origin{Kind: "scraper", InstanceID: "scraper", CandidateID: id}
	object := contentPublish(t, s, "admin", input)
	c := enrichment.PreviewCandidate{CandidateID: id, Target: w.Target, InstanceID: "scraper", Field: "lyrics", Language: "und", Format: "text", MIME: object.MIME, SizeBytes: object.Size, SHA256: object.SHA256, Origin: "sidecar", Basis: "exactStem", Status: "available", PreviewObjectID: &object.ID, ExpiresAt: time.Now().UTC().Add(enrichment.CandidateLifetime)}
	if err = s.ReserveEnrichmentPreview(ctx, w, c.SizeBytes); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishEnrichment(ctx, w, enrichment.Outcome{Candidates: []enrichment.PreviewCandidate{c}}); err != nil {
		t.Fatal(err)
	}
	return j, c
}

func TestEnrichmentApplyGuardCancellationExpiryAndReplay(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	j, c := enrichmentPreview(t, s, tracks[0])
	called := false
	guard := func(publish func() error) error { called = true; return publish() }
	if _, err := s.ApplyEnrichment(ctx, "bob", c.CandidateID, *c.PreviewObjectID, 0, domain.NewID(), guard); err == nil || called {
		t.Fatal("private candidate selected")
	}
	if _, err := s.ApplyEnrichment(ctx, "admin", c.CandidateID, "wrong-object", 0, domain.NewID(), guard); err == nil || called {
		t.Fatal("wrong file protected the selection")
	}
	if _, err := s.ApplyEnrichment(ctx, "admin", c.CandidateID, *c.PreviewObjectID, 0, domain.NewID(), func(func() error) error { return nil }); err == nil {
		t.Fatal("no-op guard selected")
	}
	first, err := s.ApplyEnrichment(ctx, "admin", c.CandidateID, *c.PreviewObjectID, 0, "apply-metadata-key", guard)
	if err != nil || !called || first.Revision != 1 || !first.Bindings[0].ManualLocked {
		t.Fatal("manual guarded selection failed", err)
	}
	if _, err = s.CancelEnrichment(ctx, "admin", j.JobID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("UPDATE enrichment_candidates SET expires_at=? WHERE id=?", time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano), c.CandidateID); err != nil {
		t.Fatal(err)
	}
	called = false
	again, err := s.ApplyEnrichment(ctx, "admin", c.CandidateID, *c.PreviewObjectID, 0, "apply-metadata-key", guard)
	if err != nil || called || !reflect.DeepEqual(first, again) {
		t.Fatal("durable successful selection did not replay", err)
	}
	_, err = s.ApplyEnrichment(ctx, "admin", c.CandidateID, *c.PreviewObjectID, 1, domain.NewID(), guard)
	matchingError(t, err, "CONFLICT")
	if called {
		t.Fatal("cancelled candidate entered file guard")
	}
	current, err := s.ContentSelection(ctx, "admin", tracks[0].ID)
	if err != nil || current.Revision != 1 {
		t.Fatal("cancel undid existing manual selection")
	}
}

func TestEnrichmentApplyUsesLatestExplicitRevisionAndCurrentAsset(t *testing.T) {
	s, items, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	_, first := enrichmentPreview(t, s, tracks[0])
	_, second := enrichmentPreview(t, s, tracks[0])
	guard := func(publish func() error) error { return publish() }
	if _, err := s.ApplyEnrichment(ctx, "admin", first.CandidateID, *first.PreviewObjectID, 0, domain.NewID(), guard); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ApplyEnrichment(ctx, "admin", second.CandidateID, *second.PreviewObjectID, 0, domain.NewID(), guard); err == nil {
		t.Fatal("stale selection revision accepted")
	}
	selected, err := s.ApplyEnrichment(ctx, "admin", second.CandidateID, *second.PreviewObjectID, 1, domain.NewID(), guard)
	if err != nil || selected.Revision != 2 {
		t.Fatal("explicit updated revision refused", err)
	}
	items[0].ObservedVersion = "v2"
	publishEvidenceTracks(t, s, items...)
	_, err = s.ApplyEnrichment(ctx, "admin", first.CandidateID, *first.PreviewObjectID, 2, domain.NewID(), guard)
	matchingError(t, err, "RESOURCE_CHANGED")
}
