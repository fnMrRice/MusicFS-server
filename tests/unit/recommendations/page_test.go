package recommendations_test

import (
	"context"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/recommendations"
)

func pageConfig(t *testing.T) recommendations.Config {
	t.Helper()
	store, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "recommendations.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return recommendations.Config{Repository: store, FailureCooldown: time.Millisecond}
}

func openPage(t *testing.T, s *recommendations.Service, refresh bool, history int) recommendations.Response {
	t.Helper()
	result, err := s.Page(t.Context(), "admin", recommendations.Request{Limit: 1, Refresh: refresh, History: history})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

type boundProvider struct {
	*accountProvider
	binding string
}

func (p *boundProvider) RecommendationAccountScope(ctx context.Context) (string, string, string, error) {
	account, revision, err := p.AccountScope(ctx)
	return account, revision, p.binding, err
}

func TestDailyRecommendationHistorySurvivesRevalidationAndIsolatesLogins(t *testing.T) {
	config := pageConfig(t)
	zone := time.FixedZone("Server", 8*3600)
	now := time.Date(2026, 11, 20, 23, 50, 0, 0, zone)
	config.Now = func() time.Time { return now }
	p := &boundProvider{accountProvider: &accountProvider{provider: &provider{fn: func(_ context.Context, _ int, n int32) (online.RecommendationPage, error) {
		return page(strconv.Itoa(int(n)), 1), nil
	}}}, binding: "first-login"}
	p.login("alice", "verified")
	r := runtimeFor("admin", "net", p)
	s := service(t, config, r)
	first := openPage(t, s, false, 0)
	for range 2 {
		openPage(t, s, false, 0)
	}
	if p.calls.Load() != 1 || first.HistoryDepth != 0 {
		t.Fatal("same-day visits refreshed recommendations")
	}
	manual := openPage(t, s, true, 0)
	now = now.Add(15 * time.Minute)
	today := openPage(t, s, false, 0)
	if today.HistoryDepth != 2 || p.calls.Load() != 3 {
		t.Fatal("first visit after server midnight did not refresh")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	p.login("alice", "revalidated")
	s = service(t, config, r)
	for n, expected := range []recommendations.Response{today, manual, first} {
		got := openPage(t, s, false, n)
		if got.HistoryDepth != 2 || got.Sources[0].Items[0].Playlist.ExternalID != expected.Sources[0].Items[0].Playlist.ExternalID || p.calls.Load() != 3 {
			t.Fatalf("restored history %d did not preserve the original page", n)
		}
	}
	p.login("", "")
	_, err := s.Page(t.Context(), "admin", recommendations.Request{Limit: 1, History: 1})
	expect(t, err, "NOT_FOUND")
	p.binding = "second-login"
	p.login("alice", "new-generation")
	if got := openPage(t, s, false, 0); got.HistoryDepth != 0 || p.calls.Load() != 4 {
		t.Fatal("fresh login reused an old account snapshot")
	}
}

func TestDailyFailureDoesNotRetryOnOpenOrConsumeHistory(t *testing.T) {
	config := pageConfig(t)
	clock := time.Now()
	config.Now = func() time.Time { return clock }
	p := &provider{fn: func(_ context.Context, _ int, n int32) (online.RecommendationPage, error) {
		if n == 2 {
			return online.RecommendationPage{}, domain.Fail("TEMPORARY_FAILURE", "Temporary failure")
		}
		return page(strconv.Itoa(int(n)), 1), nil
	}}
	s := service(t, config, runtimeFor("admin", "net", p))
	first := openPage(t, s, false, 0)
	clock = clock.AddDate(0, 0, 1)
	s = service(t, config, runtimeFor("admin", "net", p))
	for range 2 {
		got := openPage(t, s, false, 0)
		src := got.Sources[0]
		if got.HistoryDepth != 0 || src.Error == nil || !src.Stale || src.Items[0].Playlist.ExternalID != first.Sources[0].Items[0].Playlist.ExternalID || p.calls.Load() != 2 {
			t.Fatal("failed refresh lost the previous result, rotated history or retried on open")
		}
		time.Sleep(3 * time.Millisecond)
	}
	got := openPage(t, s, true, 0)
	if got.HistoryDepth != 1 || got.Sources[0].Error != nil || p.calls.Load() != 3 {
		t.Fatal("manual retry did not publish a new recommendation page")
	}
}

func TestPartialRefreshRecordsBothSourcesFromTheSameVisit(t *testing.T) {
	variable := func(_ context.Context, _ int, n int32) (online.RecommendationPage, error) {
		return page(strconv.Itoa(int(n)), 1), nil
	}
	a := &provider{fn: variable}
	b := &provider{fn: func(ctx context.Context, limit int, n int32) (online.RecommendationPage, error) {
		if n == 2 {
			return online.RecommendationPage{}, domain.Fail("TEMPORARY_FAILURE", "Temporary failure")
		}
		return variable(ctx, limit, n)
	}}
	s := service(t, pageConfig(t), runtimeFor("admin", "a", a), runtimeFor("admin", "b", b))
	first := openPage(t, s, false, 0)
	partial := openPage(t, s, true, 0)
	if partial.HistoryDepth != 1 || partial.Sources[0].Error != nil || !partial.Sources[1].Stale || partial.Sources[1].Error == nil ||
		partial.Sources[1].Items[0].Playlist.ExternalID != first.Sources[1].Items[0].Playlist.ExternalID {
		t.Fatal("partial refresh did not preserve the failed source alongside the new source")
	}
	history := openPage(t, s, false, 1)
	if history.Sources[0].Items[0].Playlist.ExternalID != first.Sources[0].Items[0].Playlist.ExternalID ||
		history.Sources[1].Error != nil || a.calls.Load() != 2 || b.calls.Load() != 2 {
		t.Fatal("history did not restore the complete preceding visit")
	}
}

func TestVisitWhilePreviousDaysRefreshIsRunning(t *testing.T) {
	config := pageConfig(t)
	clock := atomic.Int64{}
	zone := time.FixedZone("Server", 9*3600)
	clock.Store(time.Date(2026, 10, 22, 23, 59, 59, 0, zone).Unix())
	config.Now = func() time.Time { return time.Unix(clock.Load(), 0).In(zone) }
	started, finish := make(chan struct{}), make(chan struct{})
	p := &provider{fn: func(_ context.Context, _ int, n int32) (online.RecommendationPage, error) {
		if n == 1 {
			close(started)
			<-finish
		}
		return page(strconv.Itoa(int(n)), 1), nil
	}}
	s := service(t, config, runtimeFor("admin", "net", p))
	results := make(chan error, 5)
	visit := func() { _, err := s.Page(t.Context(), "admin", recommendations.Request{Limit: 1}); results <- err }
	go visit()
	receive(t, started)
	clock.Add(2)
	for range 4 {
		go visit()
	}
	close(finish)
	for range 5 {
		if err := receive(t, results); err != nil {
			t.Fatal(err)
		}
	}
	if got := openPage(t, s, false, 0); got.HistoryDepth != 1 || p.calls.Load() != 2 {
		t.Fatal("concurrent visits did not publish exactly one page per server day")
	}
}

type switchingProvider struct {
	*provider
	checks atomic.Int32
}

func (p *switchingProvider) AccountScope(context.Context) (string, string, error) {
	if n := p.checks.Add(1); n == 2 || n == 3 {
		return "private", "private-login", nil
	}
	return "", "", domain.Fail("PLATFORM_AUTH_REQUIRED", "Login required")
}

func TestIntermediateLoginCannotPublishIntoLoggedOutRecommendationScope(t *testing.T) {
	s := service(t, pageConfig(t), runtimeFor("admin", "net", &switchingProvider{provider: &provider{}}))
	_, err := s.Page(t.Context(), "admin", recommendations.Request{Limit: 1})
	expect(t, err, "RESOURCE_CHANGED")
	if res := openPage(t, s, false, 0); res.HistoryDepth != 0 || res.Sources[0].Error == nil || len(res.Sources[0].Items) != 0 {
		t.Fatal("intermediate account data reached a logged-out page")
	}
}
