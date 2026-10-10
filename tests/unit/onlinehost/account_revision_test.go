package onlinehost_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/onlinehost"
)

type revisionProvider struct {
	onlinehost.Provider
	view online.AccountView
}

func (p *revisionProvider) PlatformSession(context.Context) (online.AccountView, error) {
	return p.view, nil
}

func TestOnlineInstanceExposesOnlyCurrentValidatedAccountRevision(t *testing.T) {
	p := &revisionProvider{view: online.AccountView{State: "signedIn", AccountID: "12345", Revision: "login-1"}}
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
