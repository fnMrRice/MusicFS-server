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

	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	online "github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/adapter"
	"github.com/fnMrRice/MusicFS-server/plugins/source/online/testkit"
	domain "github.com/fnMrRice/MusicFS-server/sdk/fault"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func anonymousScoped(t *testing.T, platform string, transport http.RoundTripper) *online.Client {
	t.Helper()
	c, err := onlineplugins.New(platform, online.Scope{UserID: "user", InstanceID: "instance"}, online.Options{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func scoped(t *testing.T, platform string, transport http.RoundTripper) *online.Client {
	t.Helper()
	c := anonymousScoped(t, platform, testkit.OnlineAccountTransport(transport))
	if platform != online.AppleMusic {
		testkit.RestoreOnlineFixture(t, c, platform, "user", "instance")
	}
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
		if !strings.Contains(r.Header.Get("Cookie"), "MUSIC_U=fixture-private-token") || r.Header.Get("Authorization") != "" {
			t.Error("account request is missing its platform session")
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
		var payload struct {
			Comm    map[string]string
			Request struct {
				Method string
				Param  map[string]any
			} `json:"req_0"`
		}
		if json.NewDecoder(r.Body).Decode(&payload) != nil || r.URL.Host != "u.y.qq.com" || payload.Request.Method != "DoSearchForQQMusicDesktop" || payload.Comm["authst"] != "fixture-private-token" || payload.Comm["ct"] != "19" || payload.Request.Param["page_num"] != float64(1) {
			t.Fatal("invalid authenticated QQ desktop request")
		}
		return response(`{"code":0,"req_0":{"code":0,"data":{"code":0,"body":{"song":{"list":[{"mid":"Ab12","name":"Title &amp; Artist","singer":[{"name":"A"}],"album":{"name":"Album"},"interval":123}]}},"meta":{"curpage":1,"sum":3}}}}`).RoundTrip(r)
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
	expectCode(t, err, "PLATFORM_AUTH_REQUIRED")
	for _, capability := range c.Capabilities() {
		if capability.Supported {
			t.Fatalf("unverified Apple capability: %+v", capability)
		}
	}
	_, err = onlineplugins.New(online.Netease, online.Scope{UserID: "user", InstanceID: "instance", AccountRef: "private-account"}, online.Options{Transport: transport})
	expectCode(t, err, "UNSUPPORTED")
	if calls.Load() != 0 {
		t.Fatal("unimplemented authorization reached upstream")
	}
}

func TestAuthenticatedHTTPDoesNotFollowRedirects(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !strings.Contains(r.Header.Get("Cookie"), "MUSIC_U=fixture-private-token") {
			t.Error("platform cookie is missing")
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
