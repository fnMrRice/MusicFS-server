package pluginhost_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

type statusRepository struct{ library.Repository }

func (statusRepository) ProviderScanState(context.Context, string, string) string { return "idle" }

func TestOnlineProviderStatusIndependentOfAccountRPC(t *testing.T) {
	ctx := t.Context()
	w, dir := startOnline(t)
	gate := plugincontrol.NewGate(true)
	host, err := onlinehost.New([]onlinehost.Runtime{{Owner: "owner", InstanceID: "online", Platform: online.Netease, ConfigRevision: "1", Enabled: true, Provider: w, Gate: gate}})
	if err != nil {
		t.Fatal(err)
	}
	lib := library.New(statusRepository{}, jobs.New(nil), nil, nil)
	if err := host.ConfigureAudio(lib); err != nil {
		t.Fatal(err)
	}
	assertStatus := func(instanceStatus, providerStatus, accountState, id string) {
		t.Helper()
		instances, err := host.Instances(ctx, "owner")
		if err != nil || len(instances) != 1 || instances[0].Availability != instanceStatus || instances[0].AccountID != id {
			t.Fatalf("online instance status: %+v %v", instances, err)
		}
		providers := lib.Providers(ctx, "owner")
		if len(providers) != 1 || providers[0].Availability != providerStatus || providers[0].AccountState != accountState {
			t.Fatalf("library provider status: %+v", providers)
		}
	}
	called := filepath.Join(dir, "account-rpc")
	setControl(t, dir, fixtureControl{Started: called, Failure: "RATE_LIMITED"})
	assertStatus("available", "available", "authorized", "12345")
	if _, err := os.Stat(called); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("listing providers invoked account RPC: %v", err)
	}
	other, err := host.Instances(ctx, "other")
	if err != nil || len(other) != 0 || len(lib.Providers(ctx, "other")) != 0 {
		t.Fatal("provider status crossed owner boundary")
	}
	runtime, _ := lib.Runtime("online")
	var failure *domain.Error
	if _, err := runtime.AuthorizeAudio(ctx); !errors.As(err, &failure) || failure.Code != "RATE_LIMITED" {
		t.Fatalf("playback skipped live authorization: %v", err)
	}
	assertStatus("available", "available", "authorized", "12345")
	gate.SetAccepting(false, true)
	assertStatus("offline", "offline", "authRequired", "")
	gate.SetAccepting(true, false)
	assertStatus("available", "available", "authorized", "12345")
	signedOut := online.AccountView{State: "signedOut"}
	setControl(t, dir, fixtureControl{View: &signedOut})
	if _, err := w.PlatformAccount(ctx, online.AccountRequest{Action: "logout"}); err != nil {
		t.Fatal(err)
	}
	assertStatus("loginRequired", "unavailable", "authRequired", "")
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	assertStatus("offline", "offline", "authRequired", "")
}
