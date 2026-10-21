package recommendations_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library/recommendations"
)

func TestRecommendationNamesFollowAccountsAcrossCacheHitsAndLogout(t *testing.T) {
	p := &accountProvider{provider: &provider{}}
	var name atomic.Value
	name.Store("网易云音乐")
	r := runtimeFor("owner", "7fe223d0-a3bd-5e87-87f8-305dc26d9041", p)
	r.Name = "NetEase Cloud Music"
	r.InstanceName = func(context.Context) string { return name.Load().(string) }
	s := service(t, recommendations.Config{}, r)
	request := recommendations.Request{Limit: 1}
	if got := list(t, s, "owner", request).Sources[0]; got.Name != "网易云音乐" || got.Error == nil {
		t.Fatal("signed-out source did not display its plugin alias", got)
	}
	p.login("account-a", "generation-1")
	name.Store("Alice Listener")
	first := list(t, s, "owner", request).Sources[0]
	success(t, first)
	if first.Name != "Alice Listener" {
		t.Fatal("account name was replaced with an instance identifier", first)
	}
	name.Store("听众 Alice")
	cached := list(t, s, "owner", request).Sources[0]
	if cached.Name != "听众 Alice" || p.calls.Load() != 1 || !cached.LastSuccessAt.Equal(*first.LastSuccessAt) {
		t.Fatal("cached recommendations retained an outdated account name", cached)
	}
	p.login("", "")
	name.Store("网易云音乐")
	if got := list(t, s, "owner", request).Sources[0]; got.Name != "网易云音乐" || got.Error == nil || len(got.Items) != 0 {
		t.Fatal("logout retained the account name or its cached recommendations", got)
	}
}

func TestRecommendationPluginAliasesPermitSpacesAndUnicode(t *testing.T) {
	r := runtimeFor("owner", "7fe223d0-a3bd-5e87-87f8-305dc26d9041", &provider{})
	r.Name = "音乐来源 Music"
	s := service(t, recommendations.Config{}, r)
	if got := list(t, s, "owner", recommendations.Request{Limit: 1}).Sources[0]; got.Name != r.Name {
		t.Fatal("plugin alias was treated as an identifier", got)
	}
	r.Name = "新的来源 Music"
	if err := s.UpdateRuntime(r); err != nil {
		t.Fatal(err)
	}
	if got := list(t, s, "owner", recommendations.Request{Limit: 1}).Sources[0]; got.Name != r.Name {
		t.Fatal("replacement runtime lost its plugin alias", got)
	}
}
