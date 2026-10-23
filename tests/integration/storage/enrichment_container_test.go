package storage_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestContainerArtworkCandidatePersistsAndAppliesAutomatically(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	if _, _, err := s.QueueEnrichment(ctx, "admin", enrichmentRequest(tracks[0].ID), domain.NewID()); err != nil {
		t.Fatal(err)
	}
	work, err := s.ClaimEnrichment(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id := domain.NewID()
	input := contentInput(tracks[0], "", string(testkit.ScraperPNG()))
	input.Metadata.Field, input.Metadata.Format, input.MIME = "artwork", "png", "image/png"
	input.Metadata.Origin = mediastore.Origin{Kind: "scraper", InstanceID: "scraper", CandidateID: id}
	object := contentPublish(t, s, "admin", input)
	candidate := enrichment.PreviewCandidate{
		CandidateID: id, Target: work.Target, InstanceID: "scraper", Field: "artwork", Language: "",
		Format: "png", MIME: object.MIME, SizeBytes: object.Size, SHA256: object.SHA256, Origin: "container", Basis: "containerEmbedded",
		Status: "available", PreviewObjectID: &object.ID, ExpiresAt: time.Now().UTC().Add(enrichment.CandidateLifetime),
	}
	if err := s.ReserveEnrichmentPreview(ctx, work, candidate.SizeBytes); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*enrichment.PreviewCandidate){
		func(c *enrichment.PreviewCandidate) { c.Field = "lyrics" },
		func(c *enrichment.PreviewCandidate) { c.Basis = "directoryCover" },
		func(c *enrichment.PreviewCandidate) { c.Origin = "sidecar" },
	} {
		invalid := candidate
		mutate(&invalid)
		if err := s.FinishEnrichment(ctx, work, enrichment.Outcome{Candidates: []enrichment.PreviewCandidate{invalid}}); err == nil {
			t.Fatalf("invalid container provenance persisted: %+v", invalid)
		}
	}
	if err := s.FinishEnrichment(ctx, work, enrichment.Outcome{Candidates: []enrichment.PreviewCandidate{candidate}}); err != nil {
		t.Fatal(err)
	}
	stored, err := s.EnrichmentCandidate(ctx, "admin", candidate.CandidateID)
	if err != nil || !reflect.DeepEqual(stored, candidate) {
		t.Fatal("container candidate provenance changed in storage", stored, err)
	}
	selected, err := s.ApplyAutomaticEnrichment(ctx, "admin", candidate.CandidateID, object.ID, work.Target.Revision, domain.NewID(), false, func(publish func() error) error { return publish() })
	if err != nil || selected.Revision != work.Target.Revision+1 || len(selected.Bindings) != 1 || selected.Bindings[0].ManualLocked || selected.Bindings[0].Field != "artwork" {
		t.Fatal("container artwork automatic application failed", selected, err)
	}
}
