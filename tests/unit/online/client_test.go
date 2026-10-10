package online_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/online"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func scoped(t *testing.T, platform string, transport http.RoundTripper) *online.Client {
	t.Helper()
	c, err := online.New(platform, online.Scope{UserID: "user", InstanceID: "instance"}, online.Options{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func response(body string) http.RoundTripper {
	return roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
}

func expectCode(t *testing.T, err error, code string) {
	t.Helper()
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

const neSearch = `{"code":200,"result":{"songs":[{"id":9007199254740993,"name":"Synthetic title","artists":[{"name":"Synthetic artist"}],"album":{"name":"Synthetic album"},"duration":120001}],"songCount":2,"hasMore":true}}`

func TestSearchMappingAndScopeBoundCursor(t *testing.T) {
	var calls atomic.Int32
	transport := roundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Scheme != "https" || r.URL.Host != "music.163.com" || r.URL.Path != "/api/search/get" || r.URL.Query().Get("s") != "synthetic" {
			t.Errorf("unexpected request: %s", r.URL)
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("anonymous request has credentials")
		}
		return response(neSearch).RoundTrip(r)
	})
	c := scoped(t, online.Netease, transport)
	request := online.SearchRequest{Query: "synthetic", Limit: 1}
	page, err := c.Search(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Tracks) != 1 || page.Tracks[0].ExternalID != "9007199254740993" || *page.Tracks[0].DurationMs != 120001 || page.Tracks[0].PlaybackAvailability != "unknown" || len(page.Tracks[0].MetadataDigest) != 64 || page.Consistency != online.Unversioned || page.NextCursor == nil {
		t.Fatalf("invalid mapping: %+v", page)
	}
	encoded, _ := json.Marshal(page)
	if strings.Contains(string(encoded), "AccountRef") || strings.Contains(*page.NextCursor, "user") {
		t.Fatal("scope leaked")
	}
	request.Cursor = *page.NextCursor
	request.Query = "other"
	_, err = c.Search(context.Background(), request)
	expectCode(t, err, "INVALID_ARGUMENT")
	request.Query = "synthetic"
	other := scoped(t, online.Netease, transport)
	_, err = other.Search(context.Background(), request)
	expectCode(t, err, "INVALID_ARGUMENT")
	request.Cursor += "x"
	_, err = c.Search(context.Background(), request)
	expectCode(t, err, "INVALID_ARGUMENT")
	if calls.Load() != 1 {
		t.Fatal("invalid cursor reached upstream")
	}
}

func TestSearchQQMappingAndPartialPage(t *testing.T) {
	c := scoped(t, online.QQMusic, roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "c.y.qq.com" || r.URL.Query().Get("p") != "1" || r.Header.Get("Referer") != "https://y.qq.com/" {
			t.Error("invalid QQ request")
		}
		return response(`{"code":0,"data":{"song":{"curpage":1,"totalnum":3,"list":[{"songmid":"Ab12","songname":"Title &amp; Artist","singer":[{"name":"A"}],"albumname":"Album","interval":123}]}}}`).RoundTrip(r)
	}))
	page, err := c.Search(context.Background(), online.SearchRequest{Query: "test", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if page.Tracks[0].Title != "Title & Artist" || *page.Tracks[0].DurationMs != 123000 || page.Tracks[0].ExternalID != "Ab12" || !page.Truncated || page.NextCursor != nil {
		t.Fatalf("bad QQ mapping: %+v", page)
	}
}

func TestMetadataBoundedAndDoesNotInferAudio(t *testing.T) {
	c := scoped(t, online.Netease, response(`{"code":200,"songs":[{"id":42,"name":"Detail","ar":[],"al":{"name":"Album"},"dt":0,"h":{"br":999999},"v":42}],"privileges":[{"pl":999999,"freeTrialPrivilege":{"listenType":1}}]}`))
	track, err := c.Metadata(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if track.DurationMs != nil || track.PlaybackAvailability != "unknown" || track.SourceURL != "https://music.163.com/song?id=42" {
		t.Fatalf("invented media facts: %+v", track)
	}
	expectCode(t, c.Resolve(context.Background(), "42"), "UNSUPPORTED")
	_, err = c.Metadata(context.Background(), "https://127.0.0.1/")
	expectCode(t, err, "INVALID_ARGUMENT")
}

func TestAppleAndAccountAccessAreExplicitlyUnavailable(t *testing.T) {
	var calls atomic.Int32
	transport := roundTrip(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("must not be called")
	})
	c := scoped(t, online.AppleMusic, transport)
	_, err := c.Search(context.Background(), online.SearchRequest{Query: "synthetic", Limit: 1})
	expectCode(t, err, "AUTH_REQUIRED")
	for _, capability := range c.Capabilities() {
		if capability.Supported {
			t.Fatalf("unverified Apple capability: %+v", capability)
		}
	}
	_, err = online.New(online.Netease, online.Scope{UserID: "user", InstanceID: "instance", AccountRef: "private-account"}, online.Options{Transport: transport})
	expectCode(t, err, "UNSUPPORTED")
	if calls.Load() != 0 {
		t.Fatal("unimplemented authorization reached upstream")
	}
}

func TestAnonymousHTTPRedirectAndCookies(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Cookie") != "" {
			t.Error("cookie was accepted")
		}
		w.Header().Set("Set-Cookie", "secret=ignored")
		w.Header().Set("Location", "http://127.0.0.1/private")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	transport := roundTrip(func(r *http.Request) (*http.Response, error) {
		clone := r.Clone(r.Context())
		clone.URL.Host = u.Host
		return server.Client().Transport.RoundTrip(clone)
	})
	c := scoped(t, online.Netease, transport)
	_, err := c.Search(context.Background(), online.SearchRequest{Query: "synthetic", Limit: 1})
	expectCode(t, err, "TEMPORARY_FAILURE")
	if calls.Load() != 1 {
		t.Fatalf("expected exactly one upstream request, got %d", calls.Load())
	}
}

func TestCancellationAndRateLimitAreBounded(t *testing.T) {
	c := scoped(t, online.Netease, response(neSearch))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Search(ctx, online.SearchRequest{Query: "synthetic", Limit: 1})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	_, err = c.Search(context.Background(), online.SearchRequest{Query: "synthetic", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Search(context.Background(), online.SearchRequest{Query: "synthetic", Limit: 1})
	expectCode(t, err, "RATE_LIMITED")
	c = scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	}))
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = c.Search(ctx, online.SearchRequest{Query: "synthetic", Limit: 1})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
