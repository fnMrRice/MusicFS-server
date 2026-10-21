package onlinehost_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
)

type revisionProvider struct {
	onlinehost.Provider
	view         online.AccountView
	sessionCalls int
}

func (p *revisionProvider) PlatformSession(context.Context) (online.AccountView, error) {
	p.sessionCalls++
	return p.view, nil
}

func (p *revisionProvider) PlatformSessionSnapshot() online.AccountView { return p.view }

func TestOnlineInstanceExposesOnlyCurrentValidatedAccountRevision(t *testing.T) {
	p := &revisionProvider{view: online.AccountView{State: "signedIn", AccountID: "12345", Revision: "login-1", DisplayName: "Platform username"}}
	r := onlinehost.Runtime{Owner: "owner", InstanceID: "instance", Platform: online.Netease, ConfigRevision: "r1", Enabled: true, Provider: p}
	s, err := onlinehost.New([]onlinehost.Runtime{r})
	if err != nil {
		t.Fatal(err)
	}
	assertRevision := func(revision, availability string) {
		t.Helper()
		items, err := s.Instances(context.Background(), "owner")
		if err != nil || len(items) != 1 || items[0].AccountRevision != revision || items[0].Availability != availability {
			t.Fatal("invalid account revision projection", items, err)
		}
		if revision != "" && items[0].Name != "Platform username" {
			t.Fatal("instance name did not use its signed-in platform username", items)
		}
		if revision == "" && items[0].Name != "NetEase Cloud Music" {
			t.Fatal("unavailable account leaked a stale username or instance identifier", items)
		}
		calls := p.sessionCalls
		if got := s.InstanceName(context.Background(), "owner", "instance"); got != items[0].Name {
			t.Fatal("instance name lookup disagrees with the account list", got)
		}
		if p.sessionCalls != calls {
			t.Fatal("display-name lookup made a blocking session call")
		}
		if got := s.InstanceName(context.Background(), "other", "instance"); got != "" {
			t.Fatal("instance name lookup leaked another owner's account", got)
		}
		accountName := ""
		if revision != "" {
			accountName = "Platform username"
		}
		if got := s.AccountName(context.Background(), "owner", "instance"); got != accountName {
			t.Fatal("account name included an alias or stale identity", got)
		}
		if got := s.AccountName(context.Background(), "other", "instance"); got != "" {
			t.Fatal("account name crossed the owner boundary", got)
		}
		if p.sessionCalls != calls {
			t.Fatal("account name lookup made a blocking session call")
		}
		encoded, err := json.Marshal(items[0])
		if err != nil {
			t.Fatal(err)
		}
		var wire map[string]any
		if err := json.Unmarshal(encoded, &wire); err != nil {
			t.Fatal(err)
		}
		_, present := wire["accountRevision"]
		if present != (revision != "") || (revision != "" && items[0].AccountID != "12345") || (revision == "" && items[0].AccountID != "") {
			t.Fatal("revision did not follow the validated account identity", wire)
		}
	}
	assertRevision("login-1", "available")
	p.view.Revision = "login-2"
	assertRevision("login-2", "available")
	p.view.State = "signedOut"
	assertRevision("", "loginRequired")
	p.view.State = "signedIn"
	p.view.Revision = ""
	assertRevision("", "loginRequired")
	p.view.Revision = "login-2"
	expired := time.Now().Add(-time.Minute)
	p.view.ExpiresAt = &expired
	if got := s.AccountName(context.Background(), "owner", "instance"); got != "" {
		t.Fatal("expired account name was exposed", got)
	}
	p.view.ExpiresAt = nil
	r.Enabled = false
	if err := s.UpdateRuntime(r); err != nil {
		t.Fatal(err)
	}
	assertRevision("", "offline")
	r.Enabled, r.Provider = true, nil
	if err := s.UpdateRuntime(r); err != nil {
		t.Fatal(err)
	}
	assertRevision("", "offline")
}
