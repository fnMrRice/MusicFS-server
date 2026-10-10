package storage_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
)

func TestEvidenceConcurrentGenerationsOnlyLatestCanPublish(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	a := publishEvidenceTracks(t, s, evidenceObservation("track", "v1"))[0].Asset
	var wg sync.WaitGroup
	tickets := make(chan identification.Ticket, 8)
	errors := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			ticket, err := s.BeginEvidence(ctx, "admin", hashBinding(a))
			if err != nil {
				errors <- err
				return
			}
			tickets <- ticket
		})
	}
	wg.Wait()
	close(tickets)
	close(errors)
	for err := range errors {
		t.Fatal(err)
	}
	all := []identification.Ticket{}
	maxGeneration := int64(0)
	seen := map[int64]bool{}
	for ticket := range tickets {
		if seen[ticket.Generation] {
			t.Fatal("duplicate evidence generation")
		}
		seen[ticket.Generation] = true
		maxGeneration = max(maxGeneration, ticket.Generation)
		all = append(all, ticket)
	}
	if len(all) != 8 {
		t.Fatal("missing queued evidence task")
	}
	for _, ticket := range all {
		err := s.PublishEvidence(ctx, "admin", ticket, hashResult())
		if (err == nil) != (ticket.Generation == maxGeneration) {
			t.Fatalf("unexpected generation result: %d %v", ticket.Generation, err)
		}
	}
}

func TestEvidenceTaskAndAuditPersistAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "evidence.sqlite")
	s, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := publishEvidenceTracks(t, s, evidenceObservation("track", "v1"))[0].Asset
	ticket, err := s.BeginEvidence(ctx, "admin", hashBinding(a))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.PublishEvidence(ctx, "admin", ticket, hashResult()); err != nil {
		t.Fatal(err)
	}
	history, err := s.EvidenceHistory(ctx, "admin", a.ID, 0, 10)
	if err != nil || len(history) != 1 || history[0].Evidence.Generation != ticket.Generation || history[0].Evidence.Status != "verified" {
		t.Fatal("durable task/audit lost", err)
	}
}

func TestInvalidDeclarationRollsBackAssetReplacement(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	o := evidenceObservation("track", "v1")
	before := publishEvidenceTracks(t, s, o)[0]
	ticket, err := s.BeginEvidence(ctx, "admin", hashBinding(before.Asset))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.PublishEvidence(ctx, "admin", ticket, hashResult()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Enqueue(ctx, "admin", "local", "scan-full", domain.NewID()); err != nil {
		t.Fatal(err)
	}
	j, err := s.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	bogus := "etag-is-not-a-sha512"
	o.ObservedVersion, o.ProviderSHA512 = "v2", &bogus
	if err := s.Publish(ctx, j, []library.Observation{o}); err == nil {
		t.Fatal("invalid provider hash was accepted")
	}
	after, err := s.FindTrack(ctx, "admin", before.ID)
	if err != nil || after.AssetID != before.AssetID || after.Asset.HashStatus != "verified" {
		t.Fatal("failed scan partially replaced current evidence", err)
	}
	var count int
	if err := s.DB.QueryRow("SELECT COUNT(*) FROM source_assets WHERE track_id=?", before.ID).Scan(&count); err != nil || count != 1 {
		t.Fatal("failed scan left a partial asset", count, err)
	}
}
