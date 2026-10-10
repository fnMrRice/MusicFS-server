package recommendations_test

import (
	"context"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/recommendations"
)

type accountProvider struct {
	*provider
	mu                sync.Mutex
	account, revision string
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
