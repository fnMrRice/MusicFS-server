package plugins_test

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"testing"
	"time"
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
	if err != nil || done.State != "signedIn" {
		t.Fatalf("login: %s %v", done.State, err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	w = onlineStart(t, cfg)
	restored, err := w.PlatformAccount(ctx, online.AccountRequest{Action: "state"})
	if err != nil || restored.State != "unverified" || restored.AccountID != "12345" {
		t.Fatalf("restore: %s %v", restored.State, err)
	}
	checked, err := w.PlatformAccount(ctx, online.AccountRequest{Action: "check"})
	if err != nil || checked.State != "signedIn" {
		t.Fatalf("check: %s %v", checked.State, err)
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
	if err != nil || gone.State != "signedOut" {
		t.Fatal("logout did not survive restart")
	}
}
