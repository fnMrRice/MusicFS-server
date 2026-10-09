package storage_test

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
	"path/filepath"
	"testing"
)

func TestPersistentClaimsAndIdempotency(t *testing.T) {
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	j, merged, err := s.Enqueue(ctx, "admin", "root", "scan", "key-0001")
	if err != nil || merged {
		t.Fatal(err)
	}
	duplicate, merged, err := s.Enqueue(ctx, "admin", "root", "scan", "key-0002")
	if err != nil || !merged || duplicate.ID != j.ID {
		t.Fatal("duplicate task not merged", err)
	}
	if _, _, err = s.Enqueue(ctx, "admin", "root", "other", "key-0001"); err == nil {
		t.Fatal("conflicting parameters accepted")
	}
	claimed, err := s.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Claim(ctx); err == nil {
		t.Fatal("double execution allowed")
	}
	if err = s.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	current, err := s.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(ctx, claimed, 99, nil); err != nil {
		t.Fatal(err)
	}
	pending, _ := s.GetJob(ctx, "admin", j.ID)
	if pending.Status != "running" {
		t.Fatal("old generation published")
	}
	if err = s.Finish(ctx, current, 2, nil); err != nil {
		t.Fatal(err)
	}
	done, _ := s.GetJob(ctx, "admin", j.ID)
	if !done.Complete || done.Progress.Observed != 2 {
		t.Fatal("completion lost")
	}
}
