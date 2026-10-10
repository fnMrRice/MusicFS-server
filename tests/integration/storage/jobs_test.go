package storage_test

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
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
	j, merged, err := s.Enqueue(ctx, "admin", "root", testkit.ScanPlan("root"), "key-0001")
	if err != nil || merged {
		t.Fatal(err)
	}
	duplicate, merged, err := s.Enqueue(ctx, "admin", "root", testkit.ScanPlan("root"), "key-0002")
	if err != nil || !merged || duplicate.ID != j.ID {
		t.Fatal("duplicate task not merged", err)
	}
	changed := testkit.ScanPlan("root")
	changed.Mode = "incremental"
	if _, _, err = s.Enqueue(ctx, "admin", "root", changed, "key-0001"); err == nil {
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
	if err = s.Finish(ctx, claimed, 99, nil); err == nil {
		t.Fatal("stale generation accepted")
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
