package api_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func seedDownloadedSource(t *testing.T, store *sqlite.Store, cache *onlinecache.Manager, owner, instance, externalID string, qualities ...string) library.Track {
	t.Helper()
	ctx := t.Context()
	source, err := store.PublishOnlineTrack(ctx, owner, instance, online.Track{ExternalID: externalID, Title: "Fixture song"})
	if err != nil {
		t.Fatal(err)
	}
	scope := onlinecache.Scope{Owner: owner, Instance: instance, Source: source.ID, Plugin: "musicfs.plugin.source.netease", Account: "fixture"}
	for _, quality := range qualities {
		payload := "downloaded audio " + quality
		audio := pluginproto.OnlineAudio{ExternalID: externalID, Quality: quality, Version: "v1", MIME: "audio/mpeg", Container: "mp3", Codec: "mp3", SizeBytes: int64(len(payload))}
		if quality == "lossless" {
			audio.MIME, audio.Container, audio.Codec = "audio/flac", "flac", "flac"
		}
		body, err := cache.Open(ctx, scope, audio, onlinecache.Preferences{LimitBytes: 1 << 20}, func(context.Context) (pluginproto.OnlineAudio, io.ReadCloser, error) {
			return audio, io.NopCloser(strings.NewReader(payload)), nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := body.Close(); err != nil {
			t.Fatal(err)
		}
	}
	source, err = store.FindTrack(ctx, owner, source.ID)
	if err != nil || len(source.DownloadVersions) != len(qualities) {
		t.Fatalf("seeded %d of %d download versions: %v", len(source.DownloadVersions), len(qualities), err)
	}
	return source
}

func TestDeleteOnlineDownloadVersion(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	cacheRoot := t.TempDir()
	cache, err := onlinecache.New(t.Context(), filepath.Join(cacheRoot, "playback", "online"), store)
	if err != nil {
		t.Fatal(err)
	}
	const instance = "316664dc-7106-5acd-8df7-88c9d0d9baef"
	source := seedDownloadedSource(t, store, cache, "admin", instance, "one", "standard", "lossless")
	otherSource := seedDownloadedSource(t, store, cache, "admin", instance, "two")
	foreign := seedDownloadedSource(t, store, cache, "other", "843558e1-a158-56ac-834a-baffd12bb5c5", "one", "lossless")
	player, err := playback.New(t.Context(), store, nil, nil, playback.Config{CacheRoot: cacheRoot, MaxSessions: 4, MaxCacheBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(player.Close)
	server.RegisterPlayback(player)

	readSource := func() library.Track {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, "/api/v1/source-tracks/"+source.ID, nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("read source: %d %s", w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "SourceTrack", w.Body.Bytes())
		var result library.Track
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	versions := map[string]string{}
	for _, version := range readSource().DownloadVersions {
		versions[version.Quality] = version.ID
	}
	if versions["standard"] == "" || versions["lossless"] == "" {
		t.Fatalf("missing actual download qualities: %v", versions)
	}
	remove := func(sourceID, versionID string, authCookie *http.Cookie, originHeader, csrf string, wantStatus int) {
		t.Helper()
		r := httptest.NewRequest(http.MethodDelete, "/api/v1/source-tracks/"+sourceID+"/downloads/"+versionID, nil)
		if authCookie != nil {
			r.AddCookie(authCookie)
		}
		r.Header.Set("Origin", originHeader)
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != wantStatus {
			t.Fatalf("delete %s/%s: got %d, want %d: %s", sourceID, versionID, w.Code, wantStatus, w.Body.String())
		}
		if wantStatus != http.StatusNoContent {
			testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
		} else if w.Body.Len() != 0 {
			t.Errorf("204 returned a response body: %s", w.Body.String())
		}
	}
	lossless := versions["lossless"]
	csrf := session.CSRFToken
	remove(source.ID, lossless, nil, origin, csrf, http.StatusUnauthorized)
	remove(source.ID, lossless, cookie, origin, "", http.StatusForbidden)
	remove(source.ID, lossless, cookie, "https://other.invalid", csrf, http.StatusForbidden)
	remove(source.ID, "invalid", cookie, origin, csrf, http.StatusBadRequest)
	remove(strings.Repeat("s", 129), lossless, cookie, origin, csrf, http.StatusBadRequest)
	remove(source.ID, strings.Repeat("0", 64), cookie, origin, csrf, http.StatusNotFound)
	remove(otherSource.ID, lossless, cookie, origin, csrf, http.StatusNotFound)
	remove(foreign.ID, foreign.DownloadVersions[0].ID, cookie, origin, csrf, http.StatusNotFound)

	remove(source.ID, lossless, cookie, origin, csrf, http.StatusNoContent)
	remaining := readSource()
	if !remaining.Downloaded || len(remaining.DownloadVersions) != 1 || remaining.DownloadVersions[0].ID != versions["standard"] || remaining.DownloadVersions[0].Quality != "standard" {
		t.Fatalf("remaining download: %+v", remaining.DownloadVersions)
	}
	remove(source.ID, lossless, cookie, origin, csrf, http.StatusNotFound)
	remove(source.ID, versions["standard"], cookie, origin, csrf, http.StatusNoContent)
	if remaining := readSource(); remaining.Downloaded || len(remaining.DownloadVersions) != 0 {
		t.Fatal("source still shows downloads after deleting its last version")
	}
	untouched, err := store.FindTrack(t.Context(), "other", foreign.ID)
	if err != nil || len(untouched.DownloadVersions) != 1 || untouched.DownloadVersions[0].ID != foreign.DownloadVersions[0].ID {
		t.Fatalf("foreign download changed: %+v, %v", untouched.DownloadVersions, err)
	}
}
