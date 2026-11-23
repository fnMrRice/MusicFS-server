package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginsettings"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback/downloadjobs"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type downloadFixture struct {
	binding  onlinehost.DownloadBinding
	resolved func(string)
	save     func(context.Context, string, string) (onlinecache.Version, error)
}

func (f downloadFixture) DownloadBinding(_ context.Context, owner, instance string) (onlinehost.DownloadBinding, error) {
	if owner != "admin" || instance != "netease" {
		return onlinehost.DownloadBinding{}, domain.Fail("NOT_FOUND", "Instance not found")
	}
	return f.binding, nil
}
func (f downloadFixture) DownloadQualities(context.Context, string, string) (pluginproto.QualityOptions, error) {
	return pluginproto.QualityOptions{DefaultQuality: "lossless", Options: []pluginsettings.Option{{Value: "lossless", Label: "Lossless"}}}, nil
}
func (f downloadFixture) ResolveDownload(_ context.Context, _, _, external string, b onlinehost.DownloadBinding) (library.Track, error) {
	if f.resolved != nil {
		f.resolved(external)
	}
	if b != f.binding {
		return library.Track{}, domain.Fail("RESOURCE_CHANGED", "Changed")
	}
	return library.Track{ID: "source-" + external}, nil
}

func TestOnlineDownloadJobDisabledCacheSkipsRemainingMetadata(t *testing.T) {
	resolves, downloads := 0, 0
	f := downloadFixture{
		resolved: func(string) { resolves++ },
		save: func(context.Context, string, string) (onlinecache.Version, error) {
			downloads++
			return onlinecache.Version{}, domain.Fail("AUDIO_CACHE_DISABLED", "Cache limit is zero")
		},
	}
	dir := t.TempDir()
	s, err := downloadjobs.Open(t.Context(), dir, f, f)
	if err != nil {
		t.Fatal(err)
	}
	request := downloadjobs.Request{Quality: "lossless"}
	for i := range 10000 {
		request.ExternalIDs = append(request.ExternalIDs, strconv.Itoa(i+1))
	}
	job, err := s.Submit(t.Context(), "admin", "netease", "disabled-cache", request)
	if err != nil {
		s.Close()
		t.Fatal(err)
	}
	result := finishDownloadJob(t, s, job.ID)
	s.Close()
	if result.State != "failed" || result.Failed != len(request.ExternalIDs) || resolves != 1 || downloads != 1 {
		t.Fatalf("disabled batch: state=%s failed=%d resolves=%d downloads=%d", result.State, result.Failed, resolves, downloads)
	}
	s, err = downloadjobs.Open(t.Context(), dir, f, f)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	result, err = s.Get("admin", job.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i, item := range result.Items {
		if item.ExternalID != request.ExternalIDs[i] || item.Error == nil || item.Error.Code != "AUDIO_CACHE_DISABLED" {
			t.Fatalf("invalid disabled item %d", i)
		}
	}
}
func (f downloadFixture) DownloadOnlineTrack(ctx context.Context, _, id, quality string) (onlinecache.Version, error) {
	return f.save(ctx, id, quality)
}
func finishDownloadJob(t *testing.T, s *downloadjobs.Service, id string) downloadjobs.Job {
	t.Helper()
	for end := time.Now().Add(4 * time.Second); time.Now().Before(end); {
		job, err := s.Get("admin", id)
		if err != nil {
			t.Fatal(err)
		}
		if job.Completed+job.Failed == job.Total {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("download job did not finish")
	return downloadjobs.Job{}
}

func TestOnlineDownloadJobHTTPAndLargeIdempotentBatch(t *testing.T) {
	server, _ := testServer()
	cookie, session := login(t, server)
	gate := make(chan struct{})
	f := downloadFixture{binding: onlinehost.DownloadBinding{AccountID: "account", Revision: "v1", ConfigRevision: "cfg"}, save: func(ctx context.Context, id, quality string) (onlinecache.Version, error) {
		select {
		case <-gate:
		case <-ctx.Done():
			return onlinecache.Version{}, ctx.Err()
		}
		if id == "source-denied" {
			return onlinecache.Version{}, domain.Fail("AUDIO_CACHE_DISABLED", "Storage disabled")
		}
		if quality != "lossless" {
			return onlinecache.Version{}, domain.Fail("INVALID_ARGUMENT", "Unexpected quality")
		}
		return onlinecache.Version{ID: strings.Repeat("1", 64), Quality: "exhigh", MIME: "audio/mpeg", Codec: "mp3", Container: "mp3", SizeBytes: 128, DownloadedAt: time.Now().UTC()}, nil
	}}
	s, err := downloadjobs.Open(t.Context(), t.TempDir(), f, f)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	server.RegisterOnlineDownloadJobs(s)
	call := func(method, path, key string, body any, cs string, want int) downloadjobs.Job {
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.AddCookie(cookie)
		req.Header.Set("Origin", origin)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", cs)
		req.Header.Set("Idempotency-Key", key)
		res := httptest.NewRecorder()
		server.ServeHTTP(res, req)
		if res.Code != want {
			t.Fatalf("%s %s: got %d want %d: %s", method, path, res.Code, want, res.Body.String())
		}
		if want >= 400 {
			testkit.ValidateHTTP(t, "Error", res.Body.Bytes())
			return downloadjobs.Job{}
		}
		testkit.ValidateHTTP(t, "OnlineDownloadJob", res.Body.Bytes())
		var job downloadjobs.Job
		if err := json.Unmarshal(res.Body.Bytes(), &job); err != nil {
			t.Fatal(err)
		}
		return job
	}
	path := "/api/v1/online-instances/netease/download-jobs"
	key := "fixture-key"
	ids := make([]string, 10000)
	for i := range ids {
		ids[i] = "long-track-identifier"
	}
	ids[1] = "denied"
	input := downloadjobs.Request{ExternalIDs: ids, Quality: "lossless", AccountID: "account", AccountRevision: "v1"}
	call("POST", path, key, input, "", http.StatusForbidden)
	call("POST", path, "", input, session.CSRFToken, http.StatusBadRequest)
	stale := input
	stale.AccountRevision = "old"
	call("POST", path, key, stale, session.CSRFToken, http.StatusConflict)
	invalid := input
	invalid.Quality = "unsupported"
	call("POST", path, key, invalid, session.CSRFToken, http.StatusBadRequest)
	first := call("POST", path, key, input, session.CSRFToken, http.StatusAccepted)
	if first.Total != 2 || first.State != "queued" {
		t.Fatal(first)
	}
	if _, err := s.Get("different-owner", first.ID); err == nil {
		t.Fatal("other owner read download receipt")
	}
	if next := call("POST", path, key, input, session.CSRFToken, http.StatusAccepted); next.ID != first.ID {
		t.Fatal("idempotent replay started another job")
	}
	conflict := input
	conflict.ExternalIDs = []string{"different"}
	call("POST", path, key, conflict, session.CSRFToken, http.StatusConflict)
	close(gate)
	finishDownloadJob(t, s, first.ID)
	result := call("GET", "/api/v1/online-download-jobs/"+first.ID, "", nil, "", http.StatusOK)
	if result.State != "partial" || result.Completed != 1 || result.Failed != 1 || result.Items[0].Version.Quality != "exhigh" || result.Items[1].Error.Code != "AUDIO_CACHE_DISABLED" {
		t.Fatal(result)
	}
}

func TestOnlineDownloadJobRecoversCheckpointAndOriginalAccount(t *testing.T) {
	for _, restoredLogin := range []struct {
		account, binding string
		accept           bool
	}{
		{"user-a", "same-login", true}, {"user-a", "fresh-login", false}, {"user-b", "same-login", false},
	} {
		dir := t.TempDir()
		second := make(chan struct{})
		f := downloadFixture{binding: onlinehost.DownloadBinding{AccountID: "user-a", Revision: "rev-a", ConfigRevision: "cfg", LoginBinding: "same-login"}, save: func(ctx context.Context, id, _ string) (onlinecache.Version, error) {
			if id == "source-b" {
				close(second)
				<-ctx.Done()
				return onlinecache.Version{}, ctx.Err()
			}
			return onlinecache.Version{ID: "cached-a"}, nil
		}}
		s, err := downloadjobs.Open(t.Context(), dir, f, f)
		if err != nil {
			t.Fatal(err)
		}
		request := downloadjobs.Request{ExternalIDs: []string{"a", "b", "c"}, Quality: "lossless", AccountID: "user-a"}
		job, err := s.Submit(t.Context(), "admin", "netease", "crash-retry", request)
		if err != nil {
			s.Close()
			t.Fatal(err)
		}
		select {
		case <-second:
		case <-time.After(3 * time.Second):
			s.Close()
			t.Fatal("checkpoint not reached")
		}
		s.Close()
		f.binding.Revision = "rev-b"
		f.binding.AccountID = restoredLogin.account
		f.binding.LoginBinding = restoredLogin.binding
		var mu sync.Mutex
		var downloaded []string
		f.save = func(_ context.Context, id, q string) (onlinecache.Version, error) {
			mu.Lock()
			defer mu.Unlock()
			downloaded = append(downloaded, id)
			return onlinecache.Version{ID: id, Quality: q}, nil
		}
		s, err = downloadjobs.Open(t.Context(), dir, f, f)
		if err != nil {
			t.Fatal(err)
		}
		restored := finishDownloadJob(t, s, job.ID)
		replay, err := s.Submit(t.Context(), "admin", "netease", "crash-retry", request)
		s.Close()
		if err != nil || replay.ID != job.ID || restored.Items[0].Version.ID != "cached-a" {
			t.Fatal("lost accepted job", err)
		}
		if !restoredLogin.accept {
			if restored.State != "partial" || restored.Failed != 2 || restored.Items[1].Error.Code != "RESOURCE_CHANGED" || len(downloaded) != 0 {
				t.Fatal(restored, downloaded)
			}
		} else if restored.State != "succeeded" || !slices.Equal(downloaded, []string{"source-b", "source-c"}) {
			t.Fatal(restored, downloaded)
		}
	}
}
