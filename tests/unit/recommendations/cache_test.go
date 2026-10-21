package recommendations_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/recommendations"
)

func TestScopedCacheAndImmutableResults(t *testing.T) {
	a, b, c := &provider{}, &provider{}, &provider{}
	s := service(t, recommendations.Config{}, runtimeFor("alice", "same", a), runtimeFor("bob", "same", b), runtimeFor("alice", "other", c))
	request := recommendations.Request{InstanceIDs: []string{"same"}, Limit: 1}
	first := list(t, s, "alice", request).Sources[0]
	success(t, first)
	first.Items[0].Playlist.Title = "Mutated"
	*first.Items[0].Playlist.TrackCount = 900
	*first.LastSuccessAt = time.Time{}
	again := list(t, s, "alice", request).Sources[0]
	if again.Items[0].Playlist.Title == "Mutated" || *again.Items[0].Playlist.TrackCount != 3 || again.LastSuccessAt.IsZero() || a.calls.Load() != 1 {
		t.Fatal("cache result aliases caller memory")
	}
	list(t, s, "bob", request)
	request.Limit = 2
	list(t, s, "alice", request)
	all := list(t, s, "alice", recommendations.Request{Limit: 1})
	if len(all.Sources) != 2 || all.Sources[0].InstanceID != "same" || all.Sources[1].InstanceID != "other" || a.calls.Load() != 2 || b.calls.Load() != 1 || c.calls.Load() != 1 {
		t.Fatal("cache scope or configuration order changed")
	}
	_, err := s.List(context.Background(), "bob", recommendations.Request{InstanceIDs: []string{"other"}, Limit: 1})
	expect(t, err, "NOT_FOUND")
	if empty := list(t, s, "nobody", recommendations.Request{Limit: 1}); empty.Sources == nil || len(empty.Sources) != 0 {
		t.Fatal("empty owner should have an empty list")
	}
}

func TestFailureKeepsLastSuccessAndRefreshCooldown(t *testing.T) {
	p := &provider{fn: func(_ context.Context, _ int, n int32) (online.RecommendationPage, error) {
		if n == 2 {
			return online.RecommendationPage{}, domain.Fail("TEMPORARY_FAILURE", "private credential must not escape")
		}
		return page(fmt.Sprint(n), 1), nil
	}}
	s := service(t, recommendations.Config{TTL: 15 * time.Millisecond, FailureCooldown: 60 * time.Millisecond}, runtimeFor("owner", "one", p))
	request := recommendations.Request{Limit: 1}
	first := list(t, s, "owner", request).Sources[0]
	time.Sleep(20 * time.Millisecond)
	stale := list(t, s, "owner", request).Sources[0]
	if !stale.Stale || stale.Error == nil || stale.Error.Code != "TEMPORARY_FAILURE" || stale.LastSuccessAt == nil || !stale.LastSuccessAt.Equal(*first.LastSuccessAt) || stale.Items[0].Playlist.ExternalID != first.Items[0].Playlist.ExternalID || strings.Contains(stale.Error.Message, "credential") {
		t.Fatalf("last success was not preserved safely: %+v", stale)
	}
	request.Refresh = true
	stale.Error.Code = "MUTATED"
	for range 3 {
		if got := list(t, s, "owner", request).Sources[0]; got.Error.Code != "TEMPORARY_FAILURE" || got.Error.RetryAfter == nil {
			t.Fatal("cooldown state was mutated")
		}
	}
	if p.calls.Load() != 2 {
		t.Fatal("refresh bypassed failure cooldown")
	}
	time.Sleep(70 * time.Millisecond)
	recovered := list(t, s, "owner", request).Sources[0]
	success(t, recovered)
	if recovered.Items[0].Playlist.ExternalID == first.Items[0].Playlist.ExternalID || !recovered.LastSuccessAt.After(*first.LastSuccessAt) {
		t.Fatal("recovery did not publish new observation")
	}
}

func TestPartialFailuresAndRequestedOrder(t *testing.T) {
	good := &provider{}
	bad := &provider{fn: func(context.Context, int, int32) (online.RecommendationPage, error) {
		return online.RecommendationPage{}, domain.Fail("AUTH_REQUIRED", "secret")
	}}
	var offline *provider
	s := service(t, recommendations.Config{}, runtimeFor("owner", "good", good), runtimeFor("owner", "bad", bad), runtimeFor("owner", "offline", offline))
	r := list(t, s, "owner", recommendations.Request{InstanceIDs: []string{"bad", "good", "offline"}, Limit: 1})
	if r.Sources[0].Error.Code != "AUTH_REQUIRED" || r.Sources[0].Stale || r.Sources[0].LastSuccessAt != nil || r.Sources[2].Error.Code != "SOURCE_OFFLINE" {
		t.Fatal("source failures were not independent")
	}
	success(t, r.Sources[1])
}

func TestCacheEntryAndByteBudgetsEvictLeastRecentlyUsed(t *testing.T) {
	for _, byBytes := range []bool{false, true} {
		t.Run(fmt.Sprint(byBytes), func(t *testing.T) {
			providers := []*provider{{}, {}, {}}
			config := recommendations.Config{MaxEntries: 2}
			limit := 1
			if byBytes {
				config = recommendations.Config{MaxEntries: 8, MaxBytes: 1 << 20}
				limit = 50
				for _, p := range providers {
					p.fn = func(context.Context, int, int32) (online.RecommendationPage, error) {
						result := page("42", 50)
						for i := range result.Items {
							result.Items[i].Playlist.Title = strings.Repeat("a", 4096)
							result.Items[i].Reason = strings.Repeat("b", 4096)
						}
						return result, nil
					}
				}
			}
			s := service(t, config, runtimeFor("owner", "a", providers[0]), runtimeFor("owner", "b", providers[1]), runtimeFor("owner", "c", providers[2]))
			get := func(id string) {
				success(t, list(t, s, "owner", recommendations.Request{InstanceIDs: []string{id}, Limit: limit}).Sources[0])
			}
			get("a")
			get("b")
			get("a")
			get("c")
			get("a")
			get("b")
			if providers[0].calls.Load() != 1 || providers[1].calls.Load() != 2 || providers[2].calls.Load() != 1 {
				t.Fatal("cache did not evict the least recently used source")
			}
		})
	}
}
