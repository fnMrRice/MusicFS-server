package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/recommendations"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type recommendationHTTPProvider struct {
	calls atomic.Int32
	fail  atomic.Bool
}

func (p *recommendationHTTPProvider) PublicRecommendations(context.Context, int) (online.RecommendationPage, error) {
	p.calls.Add(1)
	if p.fail.Load() {
		return online.RecommendationPage{}, domain.Fail("TEMPORARY_FAILURE", "private upstream credential")
	}
	now := time.Now().UTC()
	count := int64(3)
	return online.RecommendationPage{Items: []online.Recommendation{{Position: 0, Reason: "Public", Playlist: online.Playlist{ExternalID: "91", Title: "Synthetic", SourceURL: "https://music.163.com/playlist?id=91", TrackCount: &count}}}, ObservedAt: now, ExpiresAt: now.Add(2 * time.Minute)}, nil
}

func TestRecommendationsHTTPGroupedCacheRefreshAndPrivacy(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, _ := login(t, server)
	p, private := &recommendationHTTPProvider{}, &recommendationHTTPProvider{}
	service, err := recommendations.New([]recommendations.Runtime{
		{Owner: "admin", InstanceID: "8df55a88-1c78-50ac-ac6b-27110484abd7", Platform: online.Netease, ConfigRevision: "rev1", Provider: p},
		{Owner: "admin", InstanceID: "7a202030-76d0-5655-b70e-aa3833cc17de", Platform: online.Netease, ConfigRevision: "rev1"},
		{Owner: "other", InstanceID: "843558e1-a158-56ac-834a-baffd12bb5c5", Platform: online.Netease, ConfigRevision: "private-revision", Provider: private},
	}, recommendations.Config{FailureCooldown: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	server.RegisterRecommendations(service)
	request := func(query string, auth bool) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/v1/recommendations"+query, nil)
		if auth {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		for _, private := range []string{"private-revision", "private upstream credential", `"assetId"`, `"streamUrl"`, `"owner"`} {
			if strings.Contains(w.Body.String(), private) {
				t.Fatal("recommendations leaked private data")
			}
		}
		return w
	}
	read := func(query string) recommendations.Response {
		t.Helper()
		w := request(query, true)
		var result recommendations.Response
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil {
			t.Fatalf("recommendations: %d %s", w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "Recommendations", w.Body.Bytes())
		return result
	}
	first := read("?instanceId=8df55a88-1c78-50ac-ac6b-27110484abd7&instanceId=7a202030-76d0-5655-b70e-aa3833cc17de&limit=1")
	if len(first.Sources) != 2 || first.Sources[0].InstanceID != "8df55a88-1c78-50ac-ac6b-27110484abd7" || len(first.Sources[0].Items) != 1 || first.Sources[0].LastSuccessAt == nil || first.Sources[1].Error == nil || first.Sources[1].Error.Code != "SOURCE_OFFLINE" || first.Sources[1].LastSuccessAt != nil {
		t.Fatalf("partial source state lost: %+v", first)
	}
	second := read("?instanceId=7a202030-76d0-5655-b70e-aa3833cc17de&instanceId=8df55a88-1c78-50ac-ac6b-27110484abd7&limit=1")
	if second.Sources[0].InstanceID != "7a202030-76d0-5655-b70e-aa3833cc17de" || second.Sources[1].InstanceID != "8df55a88-1c78-50ac-ac6b-27110484abd7" || !second.Sources[1].LastSuccessAt.Equal(*first.Sources[0].LastSuccessAt) || p.calls.Load() != 1 || private.calls.Load() != 0 {
		t.Fatal("cache or requested source order changed")
	}
	p.fail.Store(true)
	stale := read("?instanceId=8df55a88-1c78-50ac-ac6b-27110484abd7&limit=1&refresh=true").Sources[0]
	if stale.Error == nil || stale.Error.Code != "TEMPORARY_FAILURE" || !stale.Stale || len(stale.Items) != 1 || !stale.LastSuccessAt.Equal(*first.Sources[0].LastSuccessAt) || p.calls.Load() != 2 {
		t.Fatal("refresh failure erased last successful items")
	}
	if held := read("?instanceId=8df55a88-1c78-50ac-ac6b-27110484abd7&limit=1&refresh=true").Sources[0]; held.Error == nil || held.Error.RetryAfter == nil || p.calls.Load() != 2 {
		t.Fatal("refresh bypassed failure cooldown")
	}
	p.fail.Store(false)
	held := read("?instanceId=8df55a88-1c78-50ac-ac6b-27110484abd7&limit=1&refresh=true").Sources[0]
	if held.Error == nil || !held.Stale || p.calls.Load() != 2 {
		t.Fatal("provider recovery bypassed the advertised cooldown")
	}
	for _, tc := range []struct {
		query string
		auth  bool
		code  int
	}{
		{"", false, http.StatusUnauthorized}, {"?instanceId=843558e1-a158-56ac-834a-baffd12bb5c5", true, 404},
		{"?instanceId=8df55a88-1c78-50ac-ac6b-27110484abd7&instanceId=8df55a88-1c78-50ac-ac6b-27110484abd7", true, 400}, {"?instanceId=", true, 400},
		{"?limit=0", true, 400}, {"?limit=51", true, 400}, {"?limit=1&limit=2", true, 400},
		{"?refresh=1", true, 400}, {"?refresh=true&refresh=false", true, 400},
		{"?accountId=other", true, 400}, {"?cursor=unknown", true, 400},
		{"?instanceId=1&instanceId=2&instanceId=3&instanceId=4&instanceId=5&instanceId=6&instanceId=7&instanceId=8&instanceId=9", true, 400},
	} {
		w := request(tc.query, tc.auth)
		if w.Code != tc.code {
			t.Fatalf("invalid query %s: %d %s", tc.query, w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
	}
	tracks, err := store.ListTracks(context.Background(), "admin", "", "", 10)
	if err != nil || len(tracks.Items) != 0 {
		t.Fatal("recommendations imported sources", err)
	}
}
