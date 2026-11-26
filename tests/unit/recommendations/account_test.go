package recommendations_test

import (
	"context"
	"strconv"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/recommendations"
)

type accountProvider struct {
	*provider
	mu                sync.Mutex
	account, revision string
}

func TestPersonalizedResultsAreScopedToTheActiveAccount(t *testing.T) {
	p := &accountProvider{provider: &provider{fn: func(_ context.Context, _ int, call int32) (online.RecommendationPage, error) {
		p := page(strconv.Itoa(int(call)), 1)
		p.Personalized = true
		return p, nil
	}}}
	s := service(t, recommendations.Config{}, runtimeFor("owner", "provider", p))
	req := recommendations.Request{Limit: 1}
	for index, account := range []string{"first", "second"} {
		p.login(account, "revision-"+account)
		for range 2 {
			got := list(t, s, "owner", req).Sources[0]
			want := strconv.Itoa(index+1) + "0"
			if got.Error != nil || !got.Personalized || got.AccountID != account || len(got.Items) != 1 || got.Items[0].Playlist.ExternalID != want {
				t.Fatalf("unexpected scoped recommendations: %+v", got)
			}
		}
	}
	p.login("", "")
	got := list(t, s, "owner", req).Sources[0]
	if got.Personalized || len(got.Items) != 0 || got.Error == nil || got.Error.Code != "PLATFORM_AUTH_REQUIRED" {
		t.Fatalf("signed-out account retained personalized recommendations: %+v", got)
	}
}

func (p *accountProvider) AccountScope(context.Context) (string, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.account == "" {
		return "", "", domain.Fail("PLATFORM_AUTH_REQUIRED", "Sign in")
	}
	return p.account, p.revision, nil
}
func (p *accountProvider) login(account, revision string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.account, p.revision = account, revision
}

func TestRecommendationCacheRequiresCurrentLoginGeneration(t *testing.T) {
	p := &accountProvider{provider: &provider{}}
	s := service(t, recommendations.Config{}, runtimeFor("owner", "net", p))
	r := recommendations.Request{Limit: 1}
	blocked := list(t, s, "owner", r).Sources[0]
	if blocked.Error == nil || blocked.Error.Code != "PLATFORM_AUTH_REQUIRED" || p.calls.Load() != 0 {
		t.Fatal("anonymous recommendations allowed")
	}
	p.login("account-a", "generation-1")
	success(t, list(t, s, "owner", r).Sources[0])
	success(t, list(t, s, "owner", r).Sources[0])
	if p.calls.Load() != 1 {
		t.Fatal("current account cache not reused")
	}
	p.login("", "")
	blocked = list(t, s, "owner", r).Sources[0]
	if blocked.Error == nil || len(blocked.Items) != 0 || blocked.Stale || blocked.LastSuccessAt != nil {
		t.Fatal("logout exposed cached recommendation data")
	}
	p.login("account-a", "generation-2")
	res := list(t, s, "owner", r).Sources[0]
	success(t, res)
	if p.calls.Load() != 2 || res.AccountID != "account-a" {
		t.Fatal("new login reused old generation")
	}
	p.login("account-b", "generation-3")
	res = list(t, s, "owner", r).Sources[0]
	success(t, res)
	if p.calls.Load() != 3 || res.AccountID != "account-b" {
		t.Fatal("another account reused previous account data")
	}
}
