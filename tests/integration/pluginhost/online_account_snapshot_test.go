package pluginhost_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
)

func signedInDisplay() online.AccountView {
	return online.AccountView{State: "signedIn", AccountID: "12345", Revision: "login-1", DisplayName: "Platform username", LoginID: "private-login", QRURL: "https://example.invalid/login"}
}

func TestRecommendationBindingAcrossWorkerAndHost(t *testing.T) {
	w, dir := startOnline(t)
	host, err := onlinehost.New([]onlinehost.Runtime{{
		Owner: "owner", InstanceID: "instance", Platform: online.Netease, ConfigRevision: "1", Enabled: true, Provider: w,
	}})
	if err != nil {
		t.Fatal(err)
	}
	reader := host.RecommendationProvider("owner", "instance").(interface {
		RecommendationAccountScope(context.Context) (string, string, string, error)
	})
	account, revision, binding, err := reader.RecommendationAccountScope(t.Context())
	if err != nil || account != "12345" || revision != "login-1" || binding != revision {
		t.Fatal("worker without a persistent binding did not use its verified login revision")
	}
	private := strings.Repeat("3a", 32)
	setControl(t, dir, fixtureControl{RecommendationBinding: private})
	account, revision, binding, err = reader.RecommendationAccountScope(t.Context())
	if err != nil || account != "12345" || revision != "login-1" || binding != private {
		t.Fatal("host did not forward the verified recommendation binding")
	}
	public, err := w.PlatformSession(t.Context())
	encoded, _ := json.Marshal(public)
	if err != nil || strings.Contains(string(encoded), private) {
		t.Fatal("public view exposed the recommendation binding")
	}
	expired := signedInDisplay()
	past := time.Now().Add(-time.Minute)
	expired.ExpiresAt = &past
	setControl(t, dir, fixtureControl{View: &expired, RecommendationBinding: private})
	if _, _, _, err = reader.RecommendationAccountScope(t.Context()); err == nil {
		t.Fatal("host accepted a binding after account verification expired")
	}
	setControl(t, dir, fixtureControl{RecommendationBinding: "invalid"})
	if _, _, _, err = reader.RecommendationAccountScope(t.Context()); err == nil {
		t.Fatal("host accepted a malformed account binding")
	}
}

func TestAccountDisplaySnapshotTracksValidatedNamesWithoutRPC(t *testing.T) {
	w, dir := startOnline(t)
	// A failing remote call cannot affect this local projection.
	setControl(t, dir, fixtureControl{Failure: "PLATFORM_AUTH_REQUIRED"})
	view := w.PlatformSessionSnapshot()
	if view.DisplayName != "Platform username" || view.LoginID != "" || view.QRURL != "" {
		t.Fatal("snapshot did not retain only public validated identity", view)
	}
	if _, err := w.PlatformSession(t.Context()); err == nil {
		t.Fatal("expected expired platform session")
	}
	if got := w.PlatformSessionSnapshot(); got.DisplayName != "" || got.State == "signedIn" {
		t.Fatal("expired session retained display name", got)
	}
}

func TestAccountDisplaySnapshotRejectsPreLogoutAndPendingResponses(t *testing.T) {
	t.Run("stale read after logout", func(t *testing.T) {
		w, dir := startOnline(t)
		started, release := filepath.Join(dir, "started"), filepath.Join(dir, "release")
		setControl(t, dir, fixtureControl{Started: started, Release: release})
		done := make(chan error, 1)
		go func() { _, err := w.PlatformSession(t.Context()); done <- err }()
		waitFile(t, started)
		signedOut := online.AccountView{State: "signedOut"}
		setControl(t, dir, fixtureControl{View: &signedOut, Count: json.RawMessage("null")})
		if _, err := w.PlatformAccount(t.Context(), online.AccountRequest{Action: "logout"}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(release, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if w.PlatformSessionSnapshot().DisplayName != "" || w.DisplayCount() != nil {
			t.Fatal("pre-logout response restored stale display state")
		}
	})
	t.Run("read during account transition", func(t *testing.T) {
		w, dir := startOnline(t)
		started, release := filepath.Join(dir, "started"), filepath.Join(dir, "release")
		next := signedInDisplay()
		next.DisplayName, next.Revision = "Next account", "login-2"
		setControl(t, dir, fixtureControl{View: &next, Started: started, Release: release})
		done := make(chan error, 1)
		go func() { _, err := w.PlatformAccount(t.Context(), online.AccountRequest{Action: "begin"}); done <- err }()
		waitFile(t, started)
		setControl(t, dir, fixtureControl{})
		if _, err := w.PlatformSession(t.Context()); err != nil {
			t.Fatal(err)
		}
		if w.PlatformSessionSnapshot().DisplayName != "" || w.DisplayCount() != nil {
			t.Fatal("pending account change exposed old display state")
		}
		if err := os.WriteFile(release, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if got := w.PlatformSessionSnapshot(); got.DisplayName != "Next account" {
			t.Fatal("completed account change did not publish new username", got)
		}
		if count := w.DisplayCount(); count == nil || count.Value != 9 {
			t.Fatal("completed account change did not publish new count", count)
		}
	})
}

func TestAccountDisplaySnapshotClearsOnFailedMutationExpiryAndShutdown(t *testing.T) {
	w, dir := startOnline(t)
	setControl(t, dir, fixtureControl{Failure: "TEMPORARY_FAILURE"})
	if _, err := w.PlatformAccount(t.Context(), online.AccountRequest{Action: "begin"}); err == nil {
		t.Fatal("failed transition succeeded")
	}
	if w.PlatformSessionSnapshot().DisplayName != "" {
		t.Fatal("failed account transition retained username")
	}
	expired := signedInDisplay()
	past := time.Now().Add(-time.Second)
	expired.ExpiresAt = &past
	setControl(t, dir, fixtureControl{View: &expired})
	if _, err := w.PlatformSession(t.Context()); err != nil {
		t.Fatal(err)
	}
	if w.PlatformSessionSnapshot().DisplayName != "" {
		t.Fatal("expired account snapshot retained username")
	}
	setControl(t, dir, fixtureControl{})
	if _, err := w.PlatformSession(t.Context()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := w.PlatformSession(ctx); err == nil {
		t.Fatal("cancelled session call succeeded")
	}
	if w.PlatformSessionSnapshot().DisplayName == "" {
		t.Fatal("ordinary cancellation discarded valid account state")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if w.PlatformSessionSnapshot().DisplayName != "" {
		t.Fatal("stopped worker retained username")
	}
}
