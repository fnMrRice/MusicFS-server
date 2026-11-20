package storage_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

// 批量扫描必须在 WAL 自动检查点和重新打开数据库后保留任务、来源及导入触发记录。
func TestBulkScanIntegrityAcrossWALCheckpointAndReopen(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "scan.sqlite")
	store, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if store != nil {
			_ = store.Close()
		}
	}()
	var pageSize, checkpointPages int64
	if err = store.DB.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize); err != nil {
		t.Fatal(err)
	}
	if err = store.DB.QueryRowContext(ctx, "PRAGMA wal_autocheckpoint").Scan(&checkpointPages); err != nil || checkpointPages <= 0 {
		t.Fatalf("automatic WAL checkpoint unavailable: pages=%d err=%v", checkpointPages, err)
	}
	plan := testkit.ScanPlan("music")
	plan.Options.ImportLyrics, plan.Options.ImportArtwork = true, true
	const count = 758
	items := make([]library.Observation, count)
	for i := range items {
		items[i] = bulkScanObservation(i)
	}
	var completed []jobs.Job
	for pass := range 3 {
		// 第二次保持内容不变，第三次替换内容，以同时覆盖复用和历史素材失效。
		if pass == 2 {
			for i := range items {
				items[i].ObservedVersion = "version-2"
			}
		}
		queued, _, err := store.Enqueue(ctx, "admin", "local", plan, fmt.Sprintf("bulk-scan-%d", pass))
		if err != nil {
			t.Fatal(err)
		}
		job, err := store.Claim(ctx)
		if err != nil || job.ID != queued.ID {
			t.Fatalf("scan claim failed: %v", err)
		}
		if err = store.Publish(ctx, job, testkit.ScanPublication(job, items)); err != nil {
			t.Fatalf("scan publication %d failed: %v", pass, err)
		}
		assertScanIntegrity(t, store)
		if err = store.Finish(ctx, job, count, nil); err != nil {
			t.Fatalf("scan completion %d failed: %v", pass, err)
		}
		completed = append(completed, job)
	}
	// 自动检查点会把已提交页写回主库；此处不主动执行 checkpoint，避免掩盖回归。
	info, err := os.Stat(path)
	if err != nil || info.Size() < pageSize*checkpointPages {
		t.Fatalf("scan did not cross automatic WAL checkpoint: size=%v err=%v", info, err)
	}
	assertBulkScanState(t, ctx, store, completed, count)
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	assertScanIntegrity(t, store)
	assertBulkScanState(t, ctx, store, completed, count)
}

func bulkScanObservation(index int) library.Observation {
	item := evidenceObservation(fmt.Sprintf("track-%04d", index), "version-1")
	title := fmt.Sprintf("曲目 %04d %s", index, strings.Repeat("扫描完整性验证", 24))
	album, genre, codec, container := strings.Repeat("专辑资料", 24), "Electronic", "flac", "flac"
	duration, bitrate, rate, year := int64(240000), int64(950000), int64(48000), int64(2026)
	item.Title, item.Artists, item.Album, item.Genre = &title, []string{"测试艺术家", "共同演出者"}, &album, &genre
	item.Year, item.SampleRateHz, item.DurationMs = &year, &rate, &duration
	item.Codec, item.Container, item.MIME, item.SizeBytes = &codec, &container, "audio/flac", 30000000
	item.BitrateBps, item.ProbeStatus = &bitrate, "succeeded"
	now := time.Now().UTC()
	item.Evidence = []identification.Evidence{{
		Spec:   identification.Spec{Kind: "bitrate", Origin: "probe", Algorithm: "ffprobe", AlgorithmVersion: "integrity-fixture-v1", ConfigRevision: "first-audio-v1", Range: identification.Range{Unit: "milliseconds", End: &duration}},
		Result: identification.Result{Status: "reported", BitrateBps: &bitrate, BitrateMode: "unknown", ComputedAt: &now},
	}}
	return item
}

func assertScanIntegrity(t *testing.T, store *sqlite.Store) {
	t.Helper()
	var result string
	if err := store.DB.QueryRowContext(t.Context(), "PRAGMA integrity_check").Scan(&result); err != nil || result != "ok" {
		t.Fatalf("database integrity check failed: %q, %v", result, err)
	}
}

func assertBulkScanState(t *testing.T, ctx context.Context, store *sqlite.Store, completed []jobs.Job, count int) {
	t.Helper()
	for _, job := range completed {
		result, err := store.GetJob(ctx, job.Owner, job.ID)
		if err != nil || result.Status != "succeeded" || !result.Complete || result.Progress.Observed != count {
			t.Fatalf("completed scan was not preserved: %+v, %v", result, err)
		}
	}
	var tracks, triggers, activeLeases int
	if err := store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM tracks").Scan(&tracks); err != nil || tracks != count {
		t.Fatalf("unexpected track count: %d, %v", tracks, err)
	}
	if err := store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM metadata_automation").Scan(&triggers); err != nil || triggers != count*3*len(completed) {
		t.Fatalf("automatic metadata triggers were not preserved: %d, %v", triggers, err)
	}
	if err := store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM provider_scan_leases WHERE active_job_id IS NOT NULL").Scan(&activeLeases); err != nil || activeLeases != 0 {
		t.Fatalf("completed scan retained its lease: %d, %v", activeLeases, err)
	}
	if _, err := store.NextAutomation(ctx); err != nil {
		t.Fatalf("automatic metadata could not read completed scans: %v", err)
	}
}
