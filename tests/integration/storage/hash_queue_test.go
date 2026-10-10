package storage_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
)

func targetOf(t library.Track) identification.HashTarget {
	return identification.HashTarget{SourceTrackID: t.ID, AssetID: t.AssetID, AssetVersion: t.Asset.Version, FileSizeBytes: *t.Asset.FileSizeBytes}
}

func TestHashQueueConcurrentMergeRecoveryAndOldGeneration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "hash.sqlite")
	s, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	track := publishEvidenceTracks(t, s, evidenceObservation("hash", "v1"))[0]
	target := targetOf(track)
	const count = 8
	ids := make(chan string, count)
	failures := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			j, _, err := s.QueueHash(ctx, "admin", target, domain.NewID(), false)
			if err != nil {
				failures <- err
			} else {
				ids <- j.ID
			}
		}()
	}
	wg.Wait()
	close(ids)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	unique := map[string]bool{}
	for id := range ids {
		unique[id] = true
	}
	if len(unique) != 1 {
		t.Fatalf("concurrent submits created %d tasks", len(unique))
	}
	old, err := s.ClaimHash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.HashProgress(ctx, old, 123); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.RecoverHashes(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := s.ClaimHash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != old.ID || current.Generation <= old.Generation || current.Ticket.Generation <= old.Ticket.Generation || current.Ticket.ID == old.Ticket.ID || current.Progress.CompletedBytes != 0 {
		t.Fatalf("recovery reused execution proof: %+v", current)
	}
	if err = s.FinishHash(ctx, old, hashResult(), target.FileSizeBytes); err == nil {
		t.Fatal("old execution published after recovery")
	}
	if err = s.PublishEvidence(ctx, "admin", old.Ticket, hashResult()); err == nil {
		t.Fatal("old evidence published after recovery")
	}
	if err = s.FinishHash(ctx, current, hashResult(), target.FileSizeBytes); err != nil {
		t.Fatal(err)
	}
	a, _ := s.Asset(ctx, "admin", target.AssetID)
	j, _ := s.GetHashJob(ctx, "admin", current.ID)
	if a.HashStatus != "verified" || j.Status != "succeeded" {
		t.Fatal("proof and task did not commit together")
	}
	if _, err = s.GetHashJob(ctx, "another-user", current.ID); err == nil {
		t.Fatal("job leaked across owners")
	}
	if _, err = s.CancelHash(ctx, "another-user", current.ID); err == nil {
		t.Fatal("cross-owner cancellation succeeded")
	}
}

func TestHashQueueCancellationChangeAndAtomicFailure(t *testing.T) {
	ctx := context.Background()
	s := evidenceStore(t)
	o := evidenceObservation("hash", "v1")
	track := publishEvidenceTracks(t, s, o)[0]
	key := domain.NewID()
	first, _, err := s.QueueHash(ctx, "admin", targetOf(track), key, false)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, merged, err := s.QueueHash(ctx, "admin", targetOf(track), key, false)
	if err != nil || !merged || duplicate.ID != first.ID {
		t.Fatal("idempotent submit changed task", err)
	}
	old, err := s.ClaimHash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CancelHash(ctx, "admin", old.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishHash(ctx, old, hashResult(), old.FileSizeBytes); err == nil {
		t.Fatal("cancelled execution published")
	}
	second, _, err := s.QueueHash(ctx, "admin", targetOf(track), domain.NewID(), false)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID {
		t.Fatal("cancelled job was revived")
	}
	old, err = s.ClaimHash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	o.ObservedVersion = "same-size-new-bytes"
	next := publishEvidenceTracks(t, s, o)[0]
	if err = s.FinishHash(ctx, old, hashResult(), old.FileSizeBytes); err != nil {
		t.Fatal(err)
	}
	job, _ := s.GetHashJob(ctx, "admin", old.ID)
	if job.Status != "superseded" {
		t.Fatalf("changed asset task still active: %+v", job)
	}
	a, _ := s.Asset(ctx, "admin", next.AssetID)
	if a.HashStatus != "unknown" || a.FileSHA512 != nil {
		t.Fatal("old result mutated new asset")
	}
	if _, _, err = s.QueueHash(ctx, "admin", targetOf(next), key, false); err == nil {
		t.Fatal("idempotency key rebound to different asset")
	}
	if _, err = s.DB.Exec("CREATE TRIGGER reject_hash_job BEFORE INSERT ON hash_jobs BEGIN SELECT RAISE(ABORT,'test failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.QueueHash(ctx, "admin", targetOf(next), domain.NewID(), false); err == nil {
		t.Fatal("injected enqueue failure ignored")
	}
	a, _ = s.Asset(ctx, "admin", next.AssetID)
	if a.HashStatus != "unknown" {
		t.Fatal("failed enqueue left orphan pending evidence")
	}
	s.DB.Exec("DROP TRIGGER reject_hash_job")
	if _, _, err = s.QueueHash(ctx, "admin", targetOf(next), domain.NewID(), false); err != nil {
		t.Fatal(err)
	}
	active, err := s.ClaimHash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("CREATE TRIGGER reject_hash_finish BEFORE UPDATE OF state ON hash_jobs WHEN NEW.state='succeeded' BEGIN SELECT RAISE(ABORT,'test failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err = s.FinishHash(ctx, active, hashResult(), active.FileSizeBytes); err == nil {
		t.Fatal("injected finish failure ignored")
	}
	a, _ = s.Asset(ctx, "admin", next.AssetID)
	if a.HashStatus != "pending" || a.FileSHA512 != nil {
		t.Fatal("failed finish partially published verified evidence")
	}
	if err = s.FinishHash(ctx, active, hashResult(), active.FileSizeBytes-1); err == nil {
		t.Fatal("incomplete coverage accepted")
	}
}

func TestHashConflictsAndOwnerScopedEquality(t *testing.T) {
	ctx := context.Background()
	s := evidenceStore(t)
	claim := strings.Repeat("b", 128)
	one, two := evidenceObservation("a", "v1"), evidenceObservation("b", "v1")
	one.ProviderSHA512 = &claim
	tracks := publishEvidenceTracks(t, s, one, two)
	for _, track := range tracks {
		if _, _, err := s.QueueHash(ctx, "admin", targetOf(track), domain.NewID(), false); err != nil {
			t.Fatal(err)
		}
		job, err := s.ClaimHash(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.FinishHash(ctx, job, hashResult(), job.FileSizeBytes); err != nil {
			t.Fatal(err)
		}
	}
	a, err := s.Asset(ctx, "admin", tracks[0].AssetID)
	if err != nil {
		t.Fatal(err)
	}
	if a.HashConflict == nil || a.HashConflict.Reason != "PROVIDER_SHA512_MISMATCH" || *a.ProviderSHA512 != claim || *a.FileSHA512 == claim {
		t.Fatalf("claim was overwritten or conflict hidden: %+v", a)
	}
	matches, err := s.HashMatches(ctx, "admin", a.ID, "", 10)
	if err != nil || len(matches) != 1 || matches[0].SourceTrackID != tracks[1].ID {
		t.Fatal("same-byte distinct sources not identified", err)
	}
	if _, err = s.HashMatches(ctx, "another-user", a.ID, "", 10); err == nil {
		t.Fatal("byte equality leaked across users")
	}
	history, err := s.HashConflictHistory(ctx, "admin", a.ID, "", 10)
	if err != nil || len(history) != 1 || history[0].ResolvedAt != nil {
		t.Fatal("conflict audit missing", err)
	}
	for _, correction := range []*string{hashResult().SHA512, nil} {
		one.ProviderSHA512 = correction
		updated := publishEvidenceTracks(t, s, one, two)[0]
		if updated.Asset.HashConflict != nil || updated.Asset.HashStatus != "verified" {
			t.Fatal("claim correction/withdrawal corrupted proof")
		}
	}
	history, err = s.HashConflictHistory(ctx, "admin", a.ID, "", 10)
	if err != nil || len(history) != 1 || history[0].ResolvedAt == nil {
		t.Fatal("resolved conflict audit was lost", err)
	}
	if _, err = s.HashConflictHistory(ctx, "another-user", a.ID, "", 10); err == nil {
		t.Fatal("conflict audit leaked across users")
	}
	var digestBytes int
	if err = s.DB.QueryRow("SELECT length(file_sha512) FROM source_assets WHERE id=?", a.ID).Scan(&digestBytes); err != nil || digestBytes != 64 {
		t.Fatal(fmt.Sprint("invalid digest storage: ", digestBytes, " ", err))
	}
}

func TestHashExecutionRejectsForgedBindingSizeAndRange(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	track := publishEvidenceTracks(t, s, evidenceObservation("binding", "v1"))[0]
	if _, _, err := s.QueueHash(ctx, "admin", targetOf(track), domain.NewID(), false); err != nil {
		t.Fatal(err)
	}
	j, err := s.ClaimHash(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"size", "asset", "track", "version", "algorithm", "range"} {
		forged := j
		switch kind {
		case "size":
			forged.FileSizeBytes++
		case "asset":
			forged.AssetID = domain.NewID()
		case "track":
			forged.SourceTrackID = domain.NewID()
		case "version":
			forged.AssetVersion = "another-version"
		case "algorithm":
			forged.Ticket.AlgorithmVersion = "another-algorithm"
		case "range":
			end := j.FileSizeBytes - 1
			forged.Ticket.Range.End = &end
		}
		if err := s.HashProgress(ctx, forged, 1); err == nil {
			t.Fatalf("forged %s progress accepted", kind)
		}
		if err := s.FinishHash(ctx, forged, hashResult(), forged.FileSizeBytes); err == nil {
			t.Fatalf("forged %s proof accepted", kind)
		}
	}
	if err := s.HashProgress(ctx, j, j.FileSizeBytes+1); err == nil {
		t.Fatal("progress exceeded persisted total")
	}
	if err := s.FinishHash(ctx, j, hashResult(), j.FileSizeBytes); err != nil {
		t.Fatal("valid proof rejected", err)
	}
}
