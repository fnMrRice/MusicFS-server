package playback_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioffmpeg"
	streamhttp "github.com/fnMrRice/MusicFS-server/src/stream"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func cacheAudioProcessor(t *testing.T) (*audioffmpeg.Processor, audioffmpeg.Config) {
	t.Helper()
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE for actual processed cache validation")
	}
	worker := filepath.Join(t.TempDir(), "audio-worker")
	if runtime.GOOS == "windows" {
		worker += ".exe"
	}
	cmd := exec.Command("go", "build", "-mod=readonly", "-o", worker, "./src/app/musicfs-plugin-audio-ffmpeg")
	cmd.Dir = testkit.RepositoryRoot()
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build cache audio worker: %v %s", err, raw)
	}
	raw, err := os.ReadFile(worker)
	if err != nil {
		t.Fatal(err)
	}
	c := audioffmpeg.Config{FFmpegPath: ffmpeg, FFprobePath: ffprobe, WorkerPath: worker, WorkerSHA256: fmt.Sprintf("%x", sha256.Sum256(raw)), WorkDir: t.TempDir(), MaxConcurrent: 1}
	p, err := audioffmpeg.NewProcessor(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	return p, c
}

func processedBytes(t *testing.T, f recoveryFixture, d playback.Description) ([]byte, error) {
	t.Helper()
	s, err := f.player.OpenStream(context.Background(), f.who, d.SessionID, d.AttemptID, false)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	var output bytes.Buffer
	err = s.Process(&output)
	return output.Bytes(), err
}

func TestProcessedCacheActualAudioReuseSeekAndRestart(t *testing.T) {
	p, cfg := cacheAudioProcessor(t)
	f := recoverySetup(t, p, 0)
	ctx := context.Background()
	before, _ := os.ReadFile(f.path)
	d := f.create(t, "cache-first-create")
	full, err := processedBytes(t, f, d)
	if err != nil || len(full) < 1024 {
		t.Fatal("initial processing failed", err)
	}
	records, err := f.repo.ListProcessedCache(ctx)
	if err != nil || len(records) != 1 || records[0].Identity.WorkerSHA256 != cfg.WorkerSHA256 || records[0].Size != int64(len(full)) {
		t.Fatal("complete output was not published with its actual tool identity", err)
	}
	seek, err := f.player.Control(ctx, f.who, d.SessionID, "seek", playback.ControlRequest{AttemptID: d.AttemptID, PositionMs: 4000}, "cache-seek-four")
	if err != nil {
		t.Fatal(err)
	}
	tail, err := processedBytes(t, f, seek)
	if err != nil || len(tail) < 1024 || bytes.Equal(full, tail) {
		t.Fatal("position identity did not produce independent output", err)
	}
	// Only this test's freshly built worker is moved. A successful second read
	// therefore proves reuse without launching another processing worker.
	if err = os.Rename(cfg.WorkerPath, cfg.WorkerPath+".disabled"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(cfg.WorkerPath+".disabled", cfg.WorkerPath) })
	s, err := f.player.OpenStream(ctx, f.who, seek.SessionID, seek.AttemptID, false)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", seek.StreamURL, nil)
	r.Header.Set("Range", "bytes=5-9")
	if err = streamhttp.Serve(w, r, s); err != nil || w.Code != 200 || !bytes.Equal(w.Body.Bytes(), tail) {
		t.Fatal("completed cache did not reproduce the whole processed stream", err)
	}
	if w.Header().Get("Accept-Ranges") != "none" || w.Header().Get("ETag") != "" || w.Header().Get("Content-Length") != "" || seek.SeekMode != "timeRestart" {
		t.Fatal("cache falsely advertised original byte-range semantics")
	}
	changed := f.request
	changed.Preferences.GainDB = -6
	gain, err := f.player.Create(ctx, f.who, changed, "cache-gain-create")
	if err != nil {
		t.Fatal(err)
	}
	if data, err := processedBytes(t, f, gain); err == nil || len(data) != 0 {
		t.Fatal("a different gain reused an unrelated cached result")
	}
	f.player.Close()
	if err = os.Rename(cfg.WorkerPath+".disabled", cfg.WorkerPath); err != nil {
		t.Fatal(err)
	}
	p, err = audioffmpeg.NewProcessor(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.player, err = playback.New(ctx, f.repo.Store, f.repo.library, p, playback.Config{CacheRoot: filepath.Join(filepath.Dir(f.path), "cache-recovery"), MaxSessions: 8, MaxCacheBytes: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.player.Close)
	if err = os.Rename(cfg.WorkerPath, cfg.WorkerPath+".disabled"); err != nil {
		t.Fatal(err)
	}
	d = f.create(t, "cache-restart-create")
	again, err := processedBytes(t, f, d)
	if err != nil || !bytes.Equal(full, again) {
		t.Fatal("restart failed to reuse verified complete output", err)
	}
	after, _ := os.ReadFile(f.path)
	if sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("cache processing changed its source")
	}
	// The old snapshot cannot authorize reuse after the actual source changes.
	if err = os.WriteFile(f.path, shiftWave(t, before, 100), 0600); err != nil {
		t.Fatal(err)
	}
	if data, err := processedBytes(t, f, d); err == nil || len(data) != 0 {
		t.Fatal("cache ignored a changed source grant")
	}
}

type cancellingOutput struct{ cancel context.CancelFunc }

func (w cancellingOutput) Write(p []byte) (int, error) {
	w.cancel()
	return 0, context.Canceled
}

func TestProcessedCacheActualQuotaAndCancellationDoNotPublishPartial(t *testing.T) {
	p, _ := cacheAudioProcessor(t)
	f := recoverySetup(t, p, 0)
	ctx := context.Background()
	info, _ := os.Stat(f.path)
	f.player.Close()
	var err error
	root := filepath.Join(filepath.Dir(f.path), "cache-limited")
	f.player, err = playback.New(ctx, f.repo.Store, f.repo.library, p, playback.Config{CacheRoot: root, MaxSessions: 8, MaxCacheBytes: info.Size() + 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.player.Close)
	d := f.create(t, "cache-budget-audio")
	data, err := processedBytes(t, f, d)
	if err != nil || len(data) < 1024 {
		t.Fatal("cache quota failure interrupted deliverable audio", err)
	}
	assertNoProcessedCache(t, f, root)
	f.player.Close()
	root = filepath.Join(filepath.Dir(f.path), "cache-cancelled")
	f.player, err = playback.New(ctx, f.repo.Store, f.repo.library, p, playback.Config{CacheRoot: root, MaxSessions: 8, MaxCacheBytes: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.player.Close)
	d = f.create(t, "cache-cancel-audio")
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	s, err := f.player.OpenStream(streamCtx, f.who, d.SessionID, d.AttemptID, false)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Process(cancellingOutput{cancel})
	s.Close()
	if err == nil {
		t.Fatal("cancelled processing unexpectedly completed")
	}
	assertNoProcessedCache(t, f, root)
	if err = f.player.Stop(ctx, f.who, d.SessionID); err != nil {
		t.Fatal(err)
	}
}

func assertNoProcessedCache(t *testing.T, f recoveryFixture, root string) {
	t.Helper()
	items, err := f.repo.ListProcessedCache(context.Background())
	files, readErr := os.ReadDir(filepath.Join(root, "playback", "processed"))
	if err != nil || readErr != nil || len(items) != 0 || len(files) != 0 {
		t.Fatalf("partial output survived: records=%d files=%d errors=%v/%v", len(items), len(files), err, readErr)
	}
}

var _ io.Writer = cancellingOutput{}
