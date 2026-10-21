package plugins_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

func TestRealWorkerScanSnapshotLifetime(t *testing.T) {
	executable := buildWorker(t)
	body, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	roots := []sourceaccess.RootConfig{{ID: "music", Path: t.TempDir()}, {ID: "other-root", Path: t.TempDir()}}
	for _, root := range roots {
		for i := 0; i < 32; i++ {
			wave(t, filepath.Join(root.Path, fmt.Sprintf("track-%02d.wav", i)))
		}
	}
	base := pluginhost.Config{Executable: executable, SHA256: fmt.Sprintf("%x", sum), InstanceID: "5f751e43-6592-5801-9c14-9f8cd2f1cc33", UserID: "owner", Roots: roots}
	start := func(t *testing.T, cfg pluginhost.Config) (*pluginhost.Worker, context.Context) {
		t.Helper()
		cfg.StateDir = t.TempDir()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		t.Cleanup(cancel)
		worker, err := pluginhost.Start(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := worker.Close(); err != nil {
				t.Error(err)
			}
		})
		return worker, ctx
	}
	scan := func(t *testing.T, ctx context.Context, worker *pluginhost.Worker, root, cursor string) pluginproto.ScanPage {
		t.Helper()
		page, err := worker.Scan(ctx, scopedRequest(worker, pluginproto.ScanRequest{RootID: root, ScanID: "scan", Generation: 1, Limit: 1, Cursor: cursor}))
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Items) != 1 || page.NextCursor == "" || page.RootIdentity == "" {
			t.Fatalf("unexpected scan page: %+v", page)
		}
		return page
	}

	t.Run("active-pages-extend-idle", func(t *testing.T) {
		cfg := base
		cfg.SnapshotIdleTimeout, cfg.SnapshotMaxLifetime = 700*time.Millisecond, 3*time.Second
		worker, ctx := start(t, cfg)
		page := scan(t, ctx, worker, "music", "")
		started := time.Now()
		identity := page.RootIdentity
		seen := map[string]bool{page.Items[0].ExternalID: true}
		for i := 0; i < 6; i++ {
			time.Sleep(175 * time.Millisecond)
			page = scan(t, ctx, worker, "music", page.NextCursor)
			if page.RootIdentity != identity || seen[page.Items[0].ExternalID] || page.Page != i+1 {
				t.Fatal("sliding expiry changed snapshot identity or page order")
			}
			seen[page.Items[0].ExternalID] = true
		}
		if time.Since(started) <= cfg.SnapshotIdleTimeout {
			t.Fatal("test did not cross the initial idle deadline")
		}
		t.Logf("Active snapshot continued for %s across its initial %s idle deadline", time.Since(started), cfg.SnapshotIdleTimeout)
	})

	t.Run("idle-expiry-and-invalid-requests-do-not-renew", func(t *testing.T) {
		cfg := base
		cfg.SnapshotIdleTimeout, cfg.SnapshotMaxLifetime = 400*time.Millisecond, 2*time.Second
		worker, ctx := start(t, cfg)
		page := scan(t, ctx, worker, "music", "")
		time.Sleep(250 * time.Millisecond)
		if _, err := worker.Scan(ctx, scopedRequest(worker, pluginproto.ScanRequest{RootID: "other-root", ScanID: "scan", Generation: 1, Limit: 1, Cursor: page.NextCursor})); pluginproto.ErrorCode(err) != "RESOURCE_CHANGED" {
			t.Fatalf("foreign-root cursor accepted: %v", err)
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := worker.Scan(cancelled, scopedRequest(worker, pluginproto.ScanRequest{RootID: "music", ScanID: "scan", Generation: 1, Limit: 1, Cursor: page.NextCursor})); err == nil {
			t.Fatal("cancelled continuation succeeded")
		}
		time.Sleep(250 * time.Millisecond)
		if _, err := worker.Scan(ctx, scopedRequest(worker, pluginproto.ScanRequest{RootID: "music", ScanID: "scan", Generation: 1, Limit: 1, Cursor: page.NextCursor})); pluginproto.ErrorCode(err) != "RESOURCE_CHANGED" {
			t.Fatalf("idle snapshot remained alive after invalid/cancelled calls: %v", err)
		}
		scan(t, ctx, worker, "music", "")
	})

	t.Run("continuous-activity-cannot-extend-hard-deadline", func(t *testing.T) {
		cfg := base
		cfg.SnapshotIdleTimeout, cfg.SnapshotMaxLifetime = 600*time.Millisecond, 1500*time.Millisecond
		worker, ctx := start(t, cfg)
		page := scan(t, ctx, worker, "music", "")
		started := time.Now()
		for {
			time.Sleep(100 * time.Millisecond)
			next, err := worker.Scan(ctx, scopedRequest(worker, pluginproto.ScanRequest{RootID: "music", ScanID: "scan", Generation: 1, Limit: 1, Cursor: page.NextCursor}))
			if err != nil {
				if pluginproto.ErrorCode(err) != "RESOURCE_CHANGED" || time.Since(started) < cfg.SnapshotMaxLifetime-100*time.Millisecond {
					t.Fatalf("active snapshot failed before its hard limit: %v after %s", err, time.Since(started))
				}
				break
			}
			page = next
			if page.NextCursor == "" || time.Since(started) > cfg.SnapshotMaxLifetime+300*time.Millisecond {
				t.Fatal("continuous activity extended the hard snapshot lifetime")
			}
		}
		t.Logf("Continuously active snapshot expired after %s with %s hard lifetime", time.Since(started), cfg.SnapshotMaxLifetime)
	})

	t.Run("worker-owner-root-and-cursor-isolation", func(t *testing.T) {
		cfg := base
		cfg.SnapshotIdleTimeout, cfg.SnapshotMaxLifetime = 2*time.Second, 3*time.Second
		worker, ctx := start(t, cfg)
		cfg.UserID = "other-owner"
		other, _ := start(t, cfg)
		page := scan(t, ctx, worker, "music", "")
		token, _, _ := strings.Cut(page.NextCursor, ":")
		for _, tc := range []struct {
			worker             *pluginhost.Worker
			root, cursor, code string
		}{
			{other, "music", page.NextCursor, "RESOURCE_CHANGED"},
			{worker, "other-root", page.NextCursor, "RESOURCE_CHANGED"},
			{worker, "music", "missing:1", "RESOURCE_CHANGED"},
			{worker, "music", token + ":999999", "RESOURCE_CHANGED"},
			{worker, "music", "malformed", "ACCESS_DENIED"},
		} {
			if _, err := tc.worker.Scan(ctx, scopedRequest(tc.worker, pluginproto.ScanRequest{RootID: tc.root, ScanID: "scan", Generation: 1, Limit: 1, Cursor: tc.cursor})); pluginproto.ErrorCode(err) != tc.code {
				t.Fatalf("isolated scan cursor accepted: %v", err)
			}
		}
		scan(t, ctx, worker, "music", page.NextCursor)
	})

	t.Run("timeout-configuration", func(t *testing.T) {
		cfg := base
		cfg.StateDir = t.TempDir()
		for _, tc := range []struct {
			idle, lifetime time.Duration
			valid          bool
		}{
			{0, 0, true}, {time.Minute, 0, true}, {0, 10 * time.Minute, true}, {time.Second, time.Second, true},
			{-1, 0, false}, {0, -1, false}, {2 * time.Second, time.Second, false}, {0, time.Minute, false},
			{time.Minute, time.Hour + time.Nanosecond, false},
		} {
			cfg.SnapshotIdleTimeout, cfg.SnapshotMaxLifetime = tc.idle, tc.lifetime
			if err := cfg.Validate(); (err == nil) != tc.valid {
				t.Fatalf("unexpected timeout validity for %s/%s: %v", tc.idle, tc.lifetime, err)
			}
		}
	})
}
