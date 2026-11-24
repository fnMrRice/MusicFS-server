package playback_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func oldDownload(t *testing.T, cache *onlinecache.Manager, scope onlinecache.Scope, content []byte, quality string) onlinecache.Version {
	t.Helper()
	// 模拟仅保存平台描述的旧版本，特意让其与实际 PCM 文件不一致。
	audio := pluginproto.OnlineAudio{ExternalID: "42", MIME: "audio/mpeg", Codec: "mp3", Container: "mp3", Quality: quality, Version: quality, SizeBytes: int64(len(content))}
	v, err := cache.Download(t.Context(), scope, audio, 1<<20, func(context.Context) (pluginproto.OnlineAudio, io.ReadCloser, error) {
		return audio, io.NopCloser(bytes.NewReader(content)), nil
	}, nil)
	if err != nil || v.FormatMeasured {
		t.Fatalf("create original download: %+v, %v", v, err)
	}
	return v
}

func TestExistingDownloadMeasuredFromFileAfterRestart(t *testing.T) {
	f := newExplicitDownload(t)
	root := t.TempDir()
	cache, err := onlinecache.New(t.Context(), filepath.Join(root, "playback", "online"), f.store)
	if err != nil {
		t.Fatal(err)
	}
	scope := onlinecache.Scope{Owner: "admin", Plugin: "test-online", Instance: "cloud", Source: f.track.ID}
	wave, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	valid := oldDownload(t, cache, scope, wave, "original")
	invalid := oldDownload(t, cache, scope, []byte("fixture without an audio stream"), "unknown")
	// 无来源插件或授权会话可用，完整副本仍可恢复编码信息。
	player, err := playback.New(t.Context(), f.store, nil, nil, playback.Config{CacheRoot: root, MaxCacheBytes: 1 << 25})
	if err != nil {
		t.Fatal(err)
	}
	defer player.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		rows, err := f.store.ListOnlineDownloads(t.Context())
		if err != nil || len(rows) != 2 {
			t.Fatalf("read restored downloads: %+v, %v", rows, err)
		}
		updated := false
		for _, r := range rows {
			switch r.ID {
			case valid.ID:
				if !r.FormatMeasured {
					continue
				}
				if r.Codec != "pcm_s16le" || r.Container != "wav" || r.MIME != "audio/wav" || r.BitDepth != 16 ||
					r.SampleRateHz != 48000 || r.Channels != 2 || r.BitrateBps == nil || *r.BitrateBps != 1536000 ||
					!r.DownloadedAt.Equal(valid.DownloadedAt) || r.Quality != valid.Quality {
					t.Fatalf("incorrect local file profile: %+v", r)
				}
				updated = true
			case invalid.ID:
				if r.FormatMeasured || r.Codec != "mp3" || r.SampleRateHz != 0 || r.Channels != 0 {
					t.Fatalf("bad file acquired invented metadata: %+v", r)
				}
			default:
				t.Fatal("previous download identity changed", r.ID)
			}
		}
		if updated {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("existing audio metadata was not measured")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDeletingDownloadDuringLocalProbeDoesNotRestoreIt(t *testing.T) {
	f := newExplicitDownload(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	root := t.TempDir()
	old, err := onlinecache.New(ctx, root, f.store)
	if err != nil {
		t.Fatal(err)
	}
	scope := onlinecache.Scope{Owner: "admin", Plugin: "test-online", Instance: "cloud", Source: f.track.ID}
	version := oldDownload(t, old, scope, []byte("local fixture"), "original")
	started, resume := make(chan struct{}), make(chan struct{})
	cache, err := onlinecache.NewWithProbe(ctx, root, f.store, func(ctx context.Context, file *os.File, size int64) (audiotypes.Metadata, error) {
		close(started)
		select {
		case <-ctx.Done():
			return audiotypes.Metadata{}, ctx.Err()
		case <-resume:
			tag, rate := "flac", int64(44100)
			return audiotypes.Metadata{Codec: &tag, Container: &tag, MIME: "audio/flac", SampleRateHz: &rate, Channels: 2}, nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); cache.Run(ctx) }()
	defer func() { cancel(); <-done }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("metadata probe did not start")
	}
	if err := cache.Delete(ctx, scope.Owner, scope.Source, version.ID); err != nil {
		t.Fatal(err)
	}
	close(resume)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, version.ID+".audio")); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("deleted file is still pinned")
		}
		time.Sleep(10 * time.Millisecond)
	}
	rows, err := f.store.ListOnlineDownloads(ctx)
	if err != nil || len(rows) != 0 {
		t.Fatalf("deleted version was resurrected: %+v, %v", rows, err)
	}
}
