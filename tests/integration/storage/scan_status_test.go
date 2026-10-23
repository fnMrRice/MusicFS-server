package storage_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestScanStatusPrefersActiveTasksAndIsOwnerProviderScoped(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "status.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assertStatus := func(owner, provider, id string) {
		t.Helper()
		status, err := store.ScanStatus(ctx, owner, provider)
		if err != nil {
			t.Fatal(err)
		}
		if id == "" {
			if status.Job != nil {
				t.Fatalf("unexpected scan status: %+v", status.Job)
			}
		} else if status.Job == nil || status.Job.ID != id {
			t.Fatalf("scan status = %+v, want %s", status.Job, id)
		}
	}
	seed := func(owner, provider, name, state, created, updated string) jobs.Job {
		t.Helper()
		job, _, err := store.Enqueue(ctx, owner, provider, testkit.ScanPlan(name), "status-"+name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.DB.ExecContext(ctx, "UPDATE jobs SET state=?,created_at=?,updated_at=? WHERE id=?", state, created, updated, job.ID); err != nil {
			t.Fatal(err)
		}
		return job
	}
	stamp := func(second int) string { return fmt.Sprintf("2026-10-23T00:00:%02dZ", second) }
	assertStatus("admin", "local", "")
	otherOwner := seed("other", "local", "other-owner", "running", stamp(1), stamp(9))
	otherProvider := seed("admin", "remote", "other-provider", "running", stamp(1), stamp(9))
	assertStatus("admin", "local", "")
	assertStatus("other", "local", otherOwner.ID)
	assertStatus("admin", "remote", otherProvider.ID)
	queued := seed("admin", "local", "queued-first", "queued", stamp(1), stamp(2))
	second := seed("admin", "local", "queued-second", "queued", stamp(1), stamp(3))
	if second.ID < queued.ID {
		queued = second
	}
	retry := seed("admin", "local", "retry", "retry_wait", stamp(3), stamp(4))
	running := seed("admin", "local", "running", "running", stamp(4), stamp(5))
	latest := seed("admin", "local", "recent-failure", "failed", stamp(5), stamp(20))
	for range 3 {
		assertStatus("admin", "local", running.ID)
	}
	if _, err := store.DB.ExecContext(ctx, "UPDATE jobs SET state='succeeded' WHERE id=?", running.ID); err != nil {
		t.Fatal(err)
	}
	assertStatus("admin", "local", retry.ID)
	if _, err := store.DB.ExecContext(ctx, "UPDATE jobs SET state='failed' WHERE id=?", retry.ID); err != nil {
		t.Fatal(err)
	}
	assertStatus("admin", "local", queued.ID)
	if _, err := store.DB.ExecContext(ctx, "UPDATE jobs SET state='cancelled' WHERE owner='admin' AND target='local' AND state='queued'"); err != nil {
		t.Fatal(err)
	}
	assertStatus("admin", "local", latest.ID)
	// UTC 整秒和带小数的时间不能直接按包含 Z 的字符串比较。
	if _, err := store.DB.ExecContext(ctx, "UPDATE jobs SET updated_at='2026-10-23T00:00:20.001Z' WHERE id=?", running.ID); err != nil {
		t.Fatal(err)
	}
	assertStatus("admin", "local", running.ID)
	var count, attempts int
	if err := store.DB.QueryRowContext(ctx, "SELECT COUNT(*),SUM(attempt_count) FROM jobs").Scan(&count, &attempts); err != nil || count != 7 || attempts != 0 {
		t.Fatalf("status reads changed task execution: count=%d attempts=%d error=%v", count, attempts, err)
	}
}
