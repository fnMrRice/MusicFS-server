package plugins_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
)

func TestActualAccountWorkerPersistsRestoresAndLogsOut(t *testing.T) {
	path := onlineBinary(t, "./tests/testkit/online-worker", "account-fixture")
	cfg := onlineConfig(t, path, online.Netease)
	ctx := context.Background()
	w := onlineStart(t, cfg)
	begin, err := w.PlatformAccount(ctx, online.AccountRequest{Action: "begin"})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(3100 * time.Millisecond)
	done, err := w.PlatformAccount(ctx, online.AccountRequest{Action: "poll", LoginID: begin.LoginID})
	if err != nil || done.State != "signedIn" || done.AvatarURL != "https://p2.music.126.net/fixture/avatar.jpg" || done.MembershipTier != "vip" {
		t.Fatalf("login: %s %v", done.State, err)
	}
	binding := func() onlinehost.DownloadBinding {
		host, err := onlinehost.New([]onlinehost.Runtime{{Owner: cfg.UserID, InstanceID: cfg.InstanceID, Platform: cfg.Platform, ConfigRevision: "1", Enabled: true, Provider: w}})
		if err != nil {
			t.Fatal(err)
		}
		state, err := host.DownloadBinding(ctx, cfg.UserID, cfg.InstanceID)
		if err != nil || len(state.LoginBinding) != 64 {
			t.Fatalf("missing verified download binding: %v", err)
		}
		return state
	}
	original := binding()
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	w = onlineStart(t, cfg)
	page, err := w.Search(ctx, online.SearchRequest{Query: "fixture", Limit: 1})
	if err != nil || len(page.Tracks) != 1 || page.Tracks[0].ExternalID != "42" {
		t.Fatalf("first search after restart: %+v %v", page, err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	w = onlineStart(t, cfg)
	start := make(chan struct{})
	var callers sync.WaitGroup
	for range 8 {
		callers.Go(func() {
			<-start
			restored, err := w.PlatformAccount(ctx, online.AccountRequest{Action: "state"})
			if err != nil || restored.State != "signedIn" || restored.AccountID != done.AccountID || restored.AvatarURL != done.AvatarURL || restored.Revision == "" || restored.Revision == done.Revision {
				t.Errorf("restored account: %+v %v", restored, err)
			}
		})
	}
	close(start)
	callers.Wait()
	restoredLogin := binding()
	if restoredLogin.AccountID != original.AccountID || restoredLogin.Revision == original.Revision || restoredLogin.LoginBinding != original.LoginBinding {
		t.Fatal("restored credentials lost the original download identity")
	}
	_, err = w.PlatformAccount(ctx, online.AccountRequest{Action: "logout"})
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	w = onlineStart(t, cfg)
	gone, err := w.PlatformAccount(ctx, online.AccountRequest{Action: "state"})
	if err != nil || gone.State != "signedOut" || gone.AvatarURL != "" || gone.MembershipTier != "" {
		t.Fatal("logout did not survive restart")
	}
	loginOnlineFixture(t, w)
	fresh := binding()
	if fresh.AccountID != original.AccountID || fresh.LoginBinding == original.LoginBinding {
		t.Fatal("new login reused a prior download binding")
	}
}

func TestRestoredNeteaseAccountRetriesTemporaryFailureAndClearsExpiredCredentials(t *testing.T) {
	path := onlineBinary(t, "./tests/testkit/online-worker", "account-fixture")
	cfg := onlineConfig(t, path, online.Netease)
	w := onlineStart(t, cfg)
	loginOnlineFixture(t, w)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	cfg.Args = []string{"account-offline"}
	w = onlineStart(t, cfg)
	host, err := onlinehost.New([]onlinehost.Runtime{{
		Owner: cfg.UserID, InstanceID: cfg.InstanceID, Platform: cfg.Platform, ConfigRevision: "1", Enabled: true, Provider: w,
	}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	instances, err := host.Instances(ctx, cfg.UserID)
	if err != nil || len(instances) != 1 || instances[0].Availability != "offline" {
		t.Fatalf("temporary discovery failure: %+v %v", instances, err)
	}
	view, err := w.PlatformAccount(ctx, online.AccountRequest{Action: "state"})
	if err != nil || view.State != "unverified" || view.AccountID != "12345" {
		t.Fatalf("temporarily unavailable account: %+v %v", view, err)
	}
	time.Sleep(3100 * time.Millisecond)
	display, err := w.PlatformDisplaySession(ctx)
	if err != nil || display.AccountID != view.AccountID || display.Revision == "" || display.State != "signedIn" {
		t.Fatalf("first available display after credential restoration: %+v %v", display, err)
	}
	account, revision, binding, err := w.RecommendationAccountScope(ctx)
	if err != nil || account != view.AccountID || revision != display.Revision || binding == "" {
		t.Fatalf("recommendation account after retry: %q %v", account, err)
	}
	instances, err = host.Instances(ctx, cfg.UserID)
	if err != nil || len(instances) != 1 || instances[0].Availability != "available" || instances[0].AccountRevision != revision {
		t.Fatalf("discovery after retry: %+v %v", instances, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	cfg.Args = []string{"account-expired"}
	w = onlineStart(t, cfg)
	_, err = w.Search(ctx, online.SearchRequest{Query: "fixture", Limit: 1})
	onlineExpect(t, err, "PLATFORM_AUTH_REQUIRED")
	view, err = w.PlatformSession(ctx)
	if err != nil || view.State != "expired" || view.AccountID != "" {
		t.Fatalf("expired account: %+v %v", view, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.Args = nil
	w = onlineStart(t, cfg)
	view, err = w.PlatformAccount(ctx, online.AccountRequest{Action: "state"})
	if err != nil || view.State != "signedOut" || view.AccountID != "" {
		t.Fatalf("expired account after restart: %+v %v", view, err)
	}
}
