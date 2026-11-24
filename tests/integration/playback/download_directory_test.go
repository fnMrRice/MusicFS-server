package playback_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

type directoryFixture struct {
	root  string
	store *sqlite.Store
	scope onlinecache.Scope
}

func newDirectoryFixture(t *testing.T) directoryFixture {
	t.Helper()
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "downloads.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	scope := onlinecache.Scope{Owner: "admin", Instance: "cloud", Plugin: "test-online"}
	track, err := store.PublishOnlineTrack(t.Context(), scope.Owner, scope.Instance, online.Track{ExternalID: "42", Title: "Song", Artists: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	scope.Source = track.ID
	return directoryFixture{t.TempDir(), store, scope}
}

func (f directoryFixture) restart(t *testing.T) *onlinecache.Manager {
	t.Helper()
	m, err := onlinecache.New(t.Context(), f.root, f.store)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func directoryAudio(quality string) pluginproto.OnlineAudio {
	return pluginproto.OnlineAudio{ExternalID: "42", Quality: quality, Version: "stable", MIME: "audio/flac", Codec: "flac", Container: "flac", SizeBytes: 4}
}

func unexpectedDownload(context.Context) (pluginproto.OnlineAudio, io.ReadCloser, error) {
	return pluginproto.OnlineAudio{}, nil, errors.New("unexpected upstream request for downloaded audio")
}

func (f directoryFixture) download(t *testing.T, m *onlinecache.Manager, quality, content string) onlinecache.Version {
	t.Helper()
	audio := directoryAudio(quality)
	version, err := m.Download(t.Context(), f.scope, audio, 1024, func(context.Context) (pluginproto.OnlineAudio, io.ReadCloser, error) {
		return audio, io.NopCloser(strings.NewReader(content)), nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return version
}

func (f directoryFixture) playCached(t *testing.T, m *onlinecache.Manager, quality, want string) {
	t.Helper()
	body, err := m.OpenSeekable(t.Context(), f.scope, directoryAudio(quality), onlinecache.Preferences{LimitBytes: 1024}, 4, unexpectedDownload)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	var b [2]byte
	n, err := body.ReadAvailableAt(t.Context(), b[:], 1, 4)
	if n != 2 || string(b[:]) != want[1:3] || err != nil {
		t.Fatalf("read cached range = %q, %v", b, err)
	}
}

func expectDirectoryFile(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil || string(b) != want {
		t.Fatalf("file %s = %q, %v", path, b, err)
	}
}

func TestDownloadDirectoryPreservesExistingVersionsAfterRestart(t *testing.T) {
	f := newDirectoryFixture(t)
	cache := f.restart(t)
	initial := cache.DownloadDirectory()
	if initial.Path != "" || initial.EffectivePath != f.root {
		t.Fatalf("initial download directory = %+v", initial)
	}
	first := f.download(t, cache, "standard", "orig")
	// 旧索引只包含公开版本信息，不含自定义存储位置。
	legacy, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.DB.ExecContext(t.Context(), "UPDATE online_downloads SET document=? WHERE id=?", string(legacy), first.ID); err != nil {
		t.Fatal(err)
	}
	destination := t.TempDir()
	foreign := filepath.Join(destination, "partial-personal.audio")
	if err := os.WriteFile(foreign, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	custom, err := cache.SetDownloadDirectory(destination)
	resolved, resolveErr := filepath.EvalSymlinks(destination)
	if err != nil || resolveErr != nil || custom.Path != resolved {
		t.Fatalf("set download directory = %+v, %v", custom, err)
	}
	if rel, err := filepath.Rel(resolved, custom.EffectivePath); err != nil || !filepath.IsLocal(rel) {
		t.Fatalf("effective download directory %q is outside %q", custom.EffectivePath, destination)
	}
	for _, invalid := range []string{"relative", foreign} {
		if _, err := cache.SetDownloadDirectory(invalid); err == nil || cache.DownloadDirectory() != custom {
			t.Fatalf("invalid path %q changed the download directory", invalid)
		}
	}
	second := f.download(t, cache, "lossless", "next")
	firstPath := filepath.Join(f.root, first.ID+".audio")
	secondPath := filepath.Join(custom.EffectivePath, second.ID+".audio")
	expectDirectoryFile(t, firstPath, "orig")
	expectDirectoryFile(t, secondPath, "next")
	if _, err := os.Stat(filepath.Join(f.root, second.ID+".audio")); !os.IsNotExist(err) {
		t.Fatalf("new download also present in default directory: %v", err)
	}
	cache = f.restart(t)
	if got := cache.DownloadDirectory(); got != custom {
		t.Fatalf("restart changed download directory from %+v to %+v", custom, got)
	}
	f.playCached(t, cache, "standard", "orig")
	f.playCached(t, cache, "lossless", "next")
	reset, err := cache.SetDownloadDirectory("")
	if err != nil || reset != initial {
		t.Fatalf("restore default = %+v, %v", reset, err)
	}
	cache = f.restart(t)
	if got := cache.DownloadDirectory(); got != initial {
		t.Fatalf("default directory did not persist: %+v", got)
	}
	f.playCached(t, cache, "lossless", "next")
	third := f.download(t, cache, "hires", "home")
	expectDirectoryFile(t, filepath.Join(f.root, third.ID+".audio"), "home")
	if err := cache.Delete(t.Context(), f.scope.Owner, f.scope.Source, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(secondPath); !os.IsNotExist(err) {
		t.Fatalf("deleted version remains at its custom location: %v", err)
	}
	expectDirectoryFile(t, firstPath, "orig")
	expectDirectoryFile(t, foreign, "keep")
}

func TestDownloadDirectorySwitchDuringStreamingAndTrim(t *testing.T) {
	f := newDirectoryFixture(t)
	cache := f.restart(t)
	custom, err := cache.SetDownloadDirectory(filepath.Join(t.TempDir(), "new", "music"))
	if err != nil {
		t.Fatal(err)
	}
	r, w := io.Pipe()
	defer w.Close()
	audio := directoryAudio("lossless")
	body, err := cache.Open(t.Context(), f.scope, audio, onlinecache.Preferences{LimitBytes: 1024, Stream: true}, func(context.Context) (pluginproto.OnlineAudio, io.ReadCloser, error) {
		return audio, r, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	if _, err := io.WriteString(w, "ab"); err != nil {
		t.Fatal(err)
	}
	var first [2]byte
	if _, err := io.ReadFull(body, first[:]); err != nil || string(first[:]) != "ab" {
		t.Fatalf("first bytes during streaming = %q, %v", first, err)
	}
	if _, err := cache.SetDownloadDirectory(""); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "cd"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	tail, err := io.ReadAll(body)
	body.Close()
	if err != nil || string(tail) != "cd" {
		t.Fatalf("playback after directory change = %q, %v", tail, err)
	}
	cache = f.restart(t)
	version, ok := cache.CachedVersion(f.scope, audio)
	if !ok {
		t.Fatal("streaming download unavailable after directory change and restart")
	}
	path := filepath.Join(custom.EffectivePath, version.ID+".audio")
	expectDirectoryFile(t, path, "abcd")
	f.playCached(t, cache, "lossless", "abcd")
	if _, err = f.store.PutPluginSettings(t.Context(), f.scope.Owner, f.scope.Plugin, 0, settings.Values{"cacheLimitMiB": 0}); err != nil {
		t.Fatal(err)
	}
	if err = cache.Trim(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("trim left an old-directory file behind: %v", err)
	}
}

func TestDownloadDirectoryReplacesDeletedVersionDuringPlayback(t *testing.T) {
	f := newDirectoryFixture(t)
	cache := f.restart(t)
	original := f.download(t, cache, "lossless", "data")
	active, err := cache.Open(t.Context(), f.scope, directoryAudio("lossless"), onlinecache.Preferences{LimitBytes: 1024}, unexpectedDownload)
	if err != nil {
		t.Fatal(err)
	}
	defer active.Close()
	if err := cache.Delete(t.Context(), f.scope.Owner, f.scope.Source, original.ID); err != nil {
		t.Fatal(err)
	}
	selected, err := cache.SetDownloadDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	replacement := f.download(t, cache, "lossless", "data")
	if original.ID != replacement.ID {
		t.Fatal("identical audio unexpectedly changed version identity")
	}
	playing, err := io.ReadAll(active)
	active.Close()
	if err != nil || string(playing) != "data" {
		t.Fatalf("previous playback lost its audio: %q, %v", playing, err)
	}
	if _, err := os.Stat(filepath.Join(f.root, original.ID+".audio")); !os.IsNotExist(err) {
		t.Fatalf("releasing previous playback did not reclaim the deleted file: %v", err)
	}
	expectDirectoryFile(t, filepath.Join(selected.EffectivePath, replacement.ID+".audio"), "data")
	f.playCached(t, f.restart(t), "lossless", "data")
}

func TestDownloadDirectoryRestoresOfflineVolumeWithoutRedownloading(t *testing.T) {
	f := newDirectoryFixture(t)
	cache := f.restart(t)
	mount := filepath.Join(t.TempDir(), "music-disk")
	before, err := cache.SetDownloadDirectory(mount)
	if err != nil {
		t.Fatal(err)
	}
	version := f.download(t, cache, "lossless", "data")
	// 将临时目录移出原挂载位置，模拟服务器重启时外置磁盘离线。
	detached := mount + "-detached"
	if err := os.Rename(mount, detached); err != nil {
		t.Fatal(err)
	}
	cache = f.restart(t)
	if cache.DownloadDirectory() != before {
		t.Fatal("offline volume reset the configured destination")
	}
	if _, hit := cache.CachedVersion(f.scope, directoryAudio("lossless")); hit {
		t.Fatal("offline file was reported as readable")
	}
	if _, err := f.store.PutPluginSettings(t.Context(), f.scope.Owner, f.scope.Plugin, 0, settings.Values{"cacheLimitMiB": 0}); err != nil {
		t.Fatal(err)
	}
	if err := cache.Trim(t.Context()); err != nil {
		t.Fatal(err)
	}
	rows, err := f.store.ListOnlineDownloads(t.Context())
	if err != nil || len(rows) != 1 || rows[0].ID != version.ID {
		t.Fatalf("offline volume lost its saved version: %+v, %v", rows, err)
	}
	if err := os.Rename(detached, mount); err != nil {
		t.Fatal(err)
	}
	f.playCached(t, cache, "lossless", "data")
}

func TestDownloadDirectoryAlternateSpellingKeepsBothFiles(t *testing.T) {
	f := newDirectoryFixture(t)
	cache := f.restart(t)
	parent := t.TempDir()
	original, alias := filepath.Join(parent, "Music"), filepath.Join(parent, "music")
	if _, err := cache.SetDownloadDirectory(original); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(alias); os.IsNotExist(err) {
		// 在区分大小写的卷上使用目录链接来模拟同一目录的另一种拼写。
		if err := os.Symlink(original, alias); err != nil {
			t.Skipf("this volume does not support a second spelling of the same directory: %v", err)
		}
	}
	f.download(t, cache, "standard", "orig")
	if _, err := cache.SetDownloadDirectory(alias); err != nil {
		t.Fatal(err)
	}
	f.download(t, cache, "lossless", "next")
	cache = f.restart(t)
	f.playCached(t, cache, "standard", "orig")
	f.playCached(t, cache, "lossless", "next")
}

func TestDownloadDirectorySwappedVolumesKeepTheirDownloads(t *testing.T) {
	f := newDirectoryFixture(t)
	cache := f.restart(t)
	parent := t.TempDir()
	first, second, spare := filepath.Join(parent, "first"), filepath.Join(parent, "second"), filepath.Join(parent, "spare")
	if _, err := cache.SetDownloadDirectory(first); err != nil {
		t.Fatal(err)
	}
	f.download(t, cache, "standard", "orig")
	if _, err := cache.SetDownloadDirectory(second); err != nil {
		t.Fatal(err)
	}
	f.download(t, cache, "lossless", "next")
	swap := func() {
		t.Helper()
		for _, pair := range [][2]string{{first, spare}, {second, first}, {spare, second}} {
			if err := os.Rename(pair[0], pair[1]); err != nil {
				t.Fatal(err)
			}
		}
	}
	// 两块磁盘都曾由同一服务器使用。系统交换盘符时也必须识别出目录内容不匹配。
	swap()
	f.restart(t)
	swap()
	cache = f.restart(t)
	f.playCached(t, cache, "standard", "orig")
	f.playCached(t, cache, "lossless", "next")
}
