package onlinehost_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

type authenticatedBlocked struct{ *blocked }

func (p authenticatedBlocked) PlatformSession(context.Context) (online.AccountView, error) {
	return online.AccountView{State: "signedIn", AccountID: "fixture-account", Revision: "fixture-revision"}, nil
}

func TestOnlineGateDrainsExistingLeaseAndForceCancelsOnlyTarget(t *testing.T) {
	a, b := plugincontrol.NewGate(true), plugincontrol.NewGate(true)
	p := authenticatedBlocked{&blocked{entered: make(chan struct{}, 2)}}
	runtime := onlinehost.Runtime{Owner: "owner", InstanceID: "a", Platform: online.Netease, ConfigRevision: "revision", Enabled: true, Provider: p, Gate: a}
	other := runtime
	other.InstanceID = "b"
	other.Gate = b
	s, err := onlinehost.New([]onlinehost.Runtime{runtime, other})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	leaseCtx, _, release, err := s.AcquireRuntime(ctx, "owner", "a")
	if err != nil {
		t.Fatal(err)
	}
	a.SetAccepting(false, false)
	if _, err = s.Runtime(leaseCtx, "owner", "a", "fixture-account"); err != nil {
		t.Fatal("draining rejected admitted nested work", err)
	}
	_, err = s.Runtime(ctx, "owner", "a", "fixture-account")
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != "PLUGIN_DISABLED" {
		t.Fatal("new work admitted while draining", err)
	}
	if _, err = s.Runtime(ctx, "owner", "b", ""); err != nil {
		t.Fatal("another instance was disabled", err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := s.Search(leaseCtx, "owner", "a", online.SearchRequest{Query: "sample", Limit: 1})
		result <- err
	}()
	select {
	case <-p.entered:
	case <-ctx.Done():
		t.Fatal("admitted request did not start")
	}
	a.SetAccepting(false, true)
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("force did not cancel request")
	}
	if a.Active() != 1 {
		t.Fatal("outer lease released before owner cleanup", a.Active())
	}
	release()
	if a.Active() != 0 {
		t.Fatal("lease leaked")
	}
	runtime.Provider = &blocked{entered: make(chan struct{}, 1)}
	runtime.ConfigRevision = "new"
	if err = s.UpdateRuntime(runtime); err != nil {
		t.Fatal(err)
	}
	a.SetAccepting(true, false)
	r, err := s.Runtime(ctx, "owner", "a", "")
	if err != nil || r.ConfigRevision != "new" {
		t.Fatal("runtime replacement failed", r, err)
	}
}

func TestOnlineRuntimeAvailabilityPrecedesBoundAccountCheck(t *testing.T) {
	gate := plugincontrol.NewGate(false)
	r := onlinehost.Runtime{Owner: "owner", InstanceID: "a", Platform: online.Netease, ConfigRevision: "revision", Enabled: true, Gate: gate}
	s, err := onlinehost.New([]onlinehost.Runtime{r})
	if err != nil {
		t.Fatal(err)
	}
	check := func(owner, account, expected string) {
		t.Helper()
		_, err := s.Runtime(context.Background(), owner, "a", account)
		var failure *domain.Error
		if !errors.As(err, &failure) || failure.Code != expected {
			t.Fatalf("expected %s, got %v", expected, err)
		}
	}
	check("other", "fixture-account", "NOT_FOUND")
	check("owner", "fixture-account", "PLUGIN_DISABLED")
	gate.SetUnavailable("SOURCE_OFFLINE", false)
	check("owner", "fixture-account", "SOURCE_OFFLINE")
	gate.SetAccepting(true, false)
	check("owner", "fixture-account", "SOURCE_OFFLINE")
	r.Provider = authenticatedBlocked{&blocked{}}
	if err := s.UpdateRuntime(r); err != nil {
		t.Fatal(err)
	}
	check("owner", "another-account", "ACCESS_DENIED")
	if _, err := s.Runtime(context.Background(), "owner", "a", "fixture-account"); err != nil {
		t.Fatal("enabled authenticated runtime was rejected", err)
	}
}
