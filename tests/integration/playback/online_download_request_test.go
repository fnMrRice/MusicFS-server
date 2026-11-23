package playback_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

type explicitDownloadFixture struct {
	player   *playback.Service
	store    *sqlite.Store
	track    library.Track
	opened   atomic.Int32
	describe func(context.Context, string) (pluginproto.OnlineAudio, error)
	body     func() io.ReadCloser
	revoked  chan struct{}
}

func TestExplicitDownloadFinishesWhilePlaybackReaderIsPaused(t *testing.T) {
	f := newExplicitDownload(t)
	ctx := t.Context()
	if _, err := f.store.PutPluginSettings(ctx, "admin", "test-online", 0, settings.Values{"cacheLimitMiB": 0}); err != nil {
		t.Fatal(err)
	}
	cache, err := onlinecache.New(ctx, t.TempDir(), f.store)
	if err != nil {
		t.Fatal(err)
	}
	scope := onlinecache.Scope{Owner: "admin", Instance: "cloud", Plugin: "test-online", Source: f.track.ID}
	a, err := f.describe(ctx, "42")
	if err != nil {
		t.Fatal(err)
	}
	r, w := io.Pipe()
	defer w.Close()
	var opens atomic.Int32
	open := func(context.Context) (pluginproto.OnlineAudio, io.ReadCloser, error) {
		opens.Add(1)
		return a, r, nil
	}
	body, err := cache.Open(ctx, scope, a, onlinecache.Preferences{LimitBytes: 1024, Stream: true}, open)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	done := make(chan error, 1)
	go func() {
		_, err := cache.Download(ctx, scope, a, 1024, open, nil)
		done <- err
	}()
	go func() {
		_, _ = io.WriteString(w, "data")
		w.Close()
	}()
	first := make([]byte, 1)
	if _, err = io.ReadFull(body, first); err != nil || string(first) != "d" {
		t.Fatalf("playback did not receive first byte: %q, %v", first, err)
	}
	select {
	case err = <-done:
		if err != nil || opens.Load() != 1 {
			t.Fatalf("shared explicit download: opens=%d, err=%v", opens.Load(), err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("paused playback blocked the explicit download")
	}
	if err = cache.Trim(ctx); err != nil {
		t.Fatal(err)
	}
	state, err := f.store.FindTrack(ctx, "admin", f.track.ID)
	if err != nil || !state.Downloaded {
		t.Fatalf("trim dropped the paused reader's version: %+v, %v", state.DownloadVersions, err)
	}
	rest, err := io.ReadAll(body)
	if err != nil || string(rest) != "ata" {
		t.Fatalf("resumed playback: %q, %v", rest, err)
	}
	body.Close()
	if err = cache.Trim(ctx); err != nil {
		t.Fatal(err)
	}
	state, err = f.store.FindTrack(ctx, "admin", f.track.ID)
	if err != nil || state.Downloaded {
		t.Fatalf("trim did not reclaim the released version: %+v, %v", state.DownloadVersions, err)
	}
}

func newExplicitDownload(t *testing.T) *explicitDownloadFixture {
	t.Helper()
	ctx := t.Context()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	queue := jobs.New(store)
	t.Cleanup(queue.Close)
	lib := library.New(store, queue, nil, nil)
	f := &explicitDownloadFixture{store: store, revoked: make(chan struct{})}
	f.describe = func(ctx context.Context, id string) (pluginproto.OnlineAudio, error) {
		quality := provider.PlaybackQuality(ctx)
		if quality == "hires" {
			quality = "lossless"
		}
		if quality == "" {
			quality = "standard"
		}
		return pluginproto.OnlineAudio{ExternalID: id, MIME: "audio/flac", Container: "flac", Codec: "flac", SizeBytes: 4, Version: "v1", Quality: quality}, nil
	}
	f.body = func() io.ReadCloser { return io.NopCloser(bytes.NewBufferString("data")) }
	gate := plugincontrol.NewGate(true)
	if err = lib.AddAudioRuntime(library.Runtime{ID: "cloud", Owner: "admin", AudioPluginID: "test-online", SourceType: "network",
		DescribeAudio: func(ctx context.Context, id string) (pluginproto.OnlineAudio, error) { return f.describe(ctx, id) },
		OpenAudio: func(ctx context.Context, id string) (pluginproto.OnlineAudio, io.ReadCloser, error) {
			f.opened.Add(1)
			d, err := f.describe(ctx, id)
			if err != nil {
				return d, nil, err
			}
			return d, f.body(), nil
		},
		AuthorizeAudio: func(context.Context) (*plugincontrol.Gate, error) { return gate, nil },
		AudioAccount: func(context.Context) (online.AccountView, <-chan struct{}, error) {
			return online.AccountView{AccountID: "account-A", Revision: "login-1", State: "signedIn"}, f.revoked, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	f.track, err = store.PublishOnlineTrack(ctx, "admin", "cloud", online.Track{ExternalID: "42", Title: "Song", Artists: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	f.player, err = playback.New(ctx, store, lib, nil, playback.Config{CacheRoot: t.TempDir(), MaxSessions: 2, MaxCacheBytes: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.player.Close)
	return f
}

func TestExplicitDownloadReturnsActualSavedQuality(t *testing.T) {
	f := newExplicitDownload(t)
	ctx := playback.WithDownloadAccount(t.Context(), "account-A", "login-1")
	if _, err := f.store.PutPluginSettings(ctx, "admin", "test-online", 0, settings.Values{"playbackQuality": "exhigh", "streamWhileDownloading": true}); err != nil {
		t.Fatal(err)
	}
	request := func(q string) string {
		t.Helper()
		v, err := f.player.DownloadOnlineTrack(ctx, "admin", f.track.ID, q)
		want := q
		if q == "hires" {
			want = "lossless"
		}
		if err != nil || v.ID == "" || v.Quality != want || v.SizeBytes != 4 {
			t.Fatalf("download(%q) = %+v, %v", q, v, err)
		}
		return v.ID
	}
	lossless := request("hires")
	standard := request("standard")
	if standard == lossless || request("lossless") != lossless || request("standard") != standard || f.opened.Load() != 2 {
		t.Fatal("repeated or downgraded requests did not reuse the corresponding exact versions")
	}
	if pref, err := f.store.GetPluginSettings(ctx, "admin", "test-online"); err != nil || pref.Values["playbackQuality"] != "exhigh" {
		t.Fatal("download changed playback preferences", err)
	}
	for _, binding := range []struct{ account, revision string }{{"account-B", "login-1"}, {"account-A", "login-2"}} {
		stale := playback.WithDownloadAccount(ctx, binding.account, binding.revision)
		if _, err := f.player.DownloadOnlineTrack(stale, "admin", f.track.ID, "lossless"); pluginproto.ErrorCode(err) != "RESOURCE_CHANGED" {
			t.Fatalf("stale account download: %v", err)
		}
	}
	if _, err := f.player.DownloadOnlineTrack(ctx, "other", f.track.ID, "lossless"); pluginproto.ErrorCode(err) != "NOT_FOUND" {
		t.Fatalf("foreign source: %v", err)
	}
	if _, err := f.store.PutPluginSettings(ctx, "admin", "test-online", 1, settings.Values{"cacheLimitMiB": 0}); err != nil {
		t.Fatal(err)
	}
	var failure *domain.Error
	if v, err := f.player.DownloadOnlineTrack(ctx, "admin", f.track.ID, "lossless"); !errors.As(err, &failure) || failure.Code != "AUDIO_CACHE_DISABLED" || v.ID != "" || f.opened.Load() != 2 {
		t.Fatalf("zero retention reported a saved version: %+v, %v", v, err)
	}
}

func TestSavedDownloadPlaybackStopsAfterAccountRevocation(t *testing.T) {
	f := newExplicitDownload(t)
	ctx := t.Context()
	accounts := auth.New(f.store, "fixture-token", []string{"http://client.test"}, time.Hour)
	who, _, err := accounts.Login(ctx, "fixture-token", "http://client.test", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.player.DownloadOnlineTrack(ctx, "admin", f.track.ID, "standard"); err != nil {
		t.Fatal(err)
	}
	d, err := f.player.Create(ctx, who, playback.CreateRequest{SourceTrackID: f.track.ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/flac", CanPlay: "probably"}}}}, "saved-copy-revocation")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := f.player.OpenStream(ctx, who, d.SessionID, d.AttemptID, false)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	body, err := stream.OpenOnline()
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	var first [1]byte
	if _, err = io.ReadFull(body, first[:]); err != nil || first[0] != 'd' || f.opened.Load() != 1 {
		t.Fatalf("authorized cache playback: first=%q opens=%d err=%v", first, f.opened.Load(), err)
	}
	close(f.revoked)
	if n, err := body.Read(first[:]); n != 0 || pluginproto.ErrorCode(err) != "RESOURCE_CHANGED" {
		t.Fatalf("revoked playback returned cached bytes: n=%d, err=%v", n, err)
	}
	select {
	case <-stream.Context.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("account revocation did not cancel cached playback")
	}
}

func TestExplicitDownloadRequiresCompleteAuthorizedFile(t *testing.T) {
	for _, mode := range []string{"truncated", "canceled", "serviceClosed", "accountChanged"} {
		t.Run(mode, func(t *testing.T) {
			f := newExplicitDownload(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if mode == "truncated" {
				f.body = func() io.ReadCloser { return io.NopCloser(bytes.NewBufferString("bad")) }
				if v, err := f.player.DownloadOnlineTrack(ctx, "admin", f.track.ID, "lossless"); err == nil || v.ID != "" {
					t.Fatal("truncated audio was published")
				}
			} else {
				opened := make(chan struct{})
				r, w := io.Pipe()
				defer w.Close()
				f.body = func() io.ReadCloser { close(opened); return r }
				done := make(chan error, 1)
				go func() { _, err := f.player.DownloadOnlineTrack(ctx, "admin", f.track.ID, "lossless"); done <- err }()
				select {
				case <-opened:
				case <-time.After(5 * time.Second):
					t.Fatal("download did not start")
				}
				if _, err := w.Write([]byte("da")); err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "canceled":
					cancel()
				case "serviceClosed":
					f.player.Close()
				case "accountChanged":
					close(f.revoked)
				}
				select {
				case err := <-done:
					if mode != "accountChanged" && !errors.Is(err, context.Canceled) || mode == "accountChanged" && pluginproto.ErrorCode(err) != "RESOURCE_CHANGED" {
						t.Fatalf("download ended with %v", err)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("revoked download did not stop")
				}
			}
			records, err := f.store.ListOnlineDownloads(t.Context())
			if err != nil || len(records) != 0 {
				t.Fatalf("unfinished download appeared in source versions: %+v, %v", records, err)
			}
		})
	}
}
