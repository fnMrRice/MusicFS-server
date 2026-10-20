package storage_test

import (
	"context"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestScanPlanQueueMergesOnlyIdenticalScopeAndClaimsSerially(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	full := testkit.ScanPlan("b", "a")
	queued, merged, err := s.Enqueue(ctx, "admin", "local", full, "full-first")
	if err != nil || merged || queued.Generation != 0 {
		t.Fatal("new plan did not queue", err)
	}
	same, merged, err := s.Enqueue(ctx, "admin", "local", testkit.ScanPlan("a", "b"), "full-second")
	if err != nil || !merged || same.ID != queued.ID {
		t.Fatal("equivalent plan not merged", err)
	}
	active, err := s.Claim(ctx)
	if err != nil || active.Generation != 1 {
		t.Fatal("claim did not allocate generation", err)
	}
	shallow := testkit.ScanPlan("a")
	shallow.Options = pluginproto.ImportOptions{}
	next, merged, err := s.Enqueue(ctx, "admin", "local", shallow, "shallow-next")
	if err != nil || merged || next.ID == active.ID || next.Generation != 0 {
		t.Fatal("different scope merged", err)
	}
	if _, _, err = s.Enqueue(ctx, "admin", "local", shallow, "full-first"); err == nil {
		t.Fatal("changed plan reused idempotency key")
	}
	if _, err = s.Claim(ctx); err == nil {
		t.Fatal("same provider received concurrent scan leases")
	}
	if _, _, err = s.Enqueue(ctx, "admin", "other", shallow, "other-provider"); err != nil {
		t.Fatal(err)
	}
	other, err := s.Claim(ctx)
	if err != nil || other.ProviderInstanceID != "other" {
		t.Fatal("active provider blocked another provider", err)
	}
	if err = s.Publish(ctx, active, testkit.ScanPublication(active, nil)); err != nil {
		t.Fatal("queued scope invalidated active publication", err)
	}
	if err = s.Finish(ctx, active, 0, nil); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.Claim(ctx)
	if err != nil || claimed.ID != next.ID || claimed.Generation != 2 {
		t.Fatal("next scope not claimed", err)
	}
	if claimed.LastSuccessAt != nil {
		t.Fatal("success from another scope was reused")
	}
	if err = s.Finish(ctx, claimed, 0, nil); err != nil {
		t.Fatal(err)
	}
	repeat, _, err := s.Enqueue(ctx, "admin", "local", full, "full-repeat")
	if err != nil || repeat.LastSuccessAt == nil {
		t.Fatal("same plan lost last successful scan", err)
	}
	if err = s.Finish(ctx, other, 0, nil); err != nil {
		t.Fatal(err)
	}
}

func TestScanPlansPersistAndRecoveryRevokesOldPublication(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "plans.db")
	s, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	plan := testkit.ScanPlan("music")
	plan.Options.MaxDepth = 3
	job, _, err := s.Enqueue(ctx, "admin", "local", plan, "durable-plan")
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.RecoverJobs(ctx); err != nil {
		t.Fatal(err)
	}
	pending, err := s.GetJob(ctx, "admin", job.ID)
	if err != nil || pending.Generation != 0 || !reflect.DeepEqual(pending.Plan, old.Plan) || pending.PlanDigest != old.PlanDigest {
		t.Fatal("persisted plan changed on restart", err)
	}
	if err = s.Publish(ctx, old, testkit.ScanPublication(old, nil)); err == nil {
		t.Fatal("recovered old worker published")
	}
	current := claimScanWhenDue(t, s)
	if current.Generation <= old.Generation {
		t.Fatal("recovery reused generation")
	}
	if err = s.Finish(ctx, old, 1, nil); err == nil {
		t.Fatal("recovered old worker finished new execution")
	}
	if err = s.Publish(ctx, current, testkit.ScanPublication(current, nil)); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(ctx, current, 0, nil); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentScanSubmissionAndClaimHaveSingleOwner(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make(chan jobs.Job, 8)
	failures := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			j, _, err := s.Enqueue(ctx, "admin", "local", testkit.ScanPlan("music"), domain.NewID())
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
			t.Fatal("concurrent identical plans split")
		}
		id = j.ID
	}
	claims := make(chan jobs.Job, 8)
	for range 8 {
		wg.Go(func() {
			if j, err := s.Claim(ctx); err == nil {
				claims <- j
			}
		})
	}
	wg.Wait()
	close(claims)
	if len(claims) != 1 {
		t.Fatalf("expected one scan lease, got %d", len(claims))
	}
}
