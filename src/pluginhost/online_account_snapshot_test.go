package pluginhost

import (
	"context"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

func signedInDisplay() online.AccountView {
	return online.AccountView{State: "signedIn", AccountID: "12345",
		Revision: "login-1", DisplayName: "Platform username",
		LoginID: "private-login", QRURL: "https://example.invalid/login"}
}

func TestAccountDisplaySnapshotTracksValidatedNamesWithoutRPC(t *testing.T) {
	w := &OnlineWorker{done: make(chan struct{})}
	generation := w.accountDisplay.begin(true)
	w.accountDisplay.finish(generation, true, signedInDisplay(), true)
	// No peer is installed: this projection must never contact the worker.
	view := w.PlatformSessionSnapshot()
	if view.DisplayName != "Platform username" || view.LoginID != "" || view.QRURL != "" {
		t.Fatal("snapshot did not retain only the public validated identity", view)
	}
	w.accountDisplay.invalidateOnError(domain.Fail("PLATFORM_AUTH_REQUIRED", "Expired"))
	if got := w.PlatformSessionSnapshot(); got.DisplayName != "" || got.State == "signedIn" {
		t.Fatal("expired session retained its display name", got)
	}
}

func TestAccountDisplaySnapshotRejectsPreLogoutAndPendingResponses(t *testing.T) {
	var state accountDisplaySnapshot
	initial := state.begin(true)
	state.finish(initial, true, signedInDisplay(), true)
	stale := state.begin(false)
	state.invalidate()
	state.finish(stale, false, signedInDisplay(), true)
	if got := state.current(); got.DisplayName != "" {
		t.Fatal("pre-logout response restored a stale username", got)
	}
	changing := state.begin(true)
	concurrent := state.begin(false)
	state.finish(concurrent, false, signedInDisplay(), true)
	if got := state.current(); got.DisplayName != "" {
		t.Fatal("pending account change exposed an old username", got)
	}
	next := signedInDisplay()
	next.DisplayName, next.Revision = "Next account", "login-2"
	state.finish(changing, true, next, true)
	if got := state.current(); got.DisplayName != "Next account" {
		t.Fatal("completed account change did not publish the new username", got)
	}
}

func TestAccountDisplaySnapshotClearsOnFailedMutationExpiryAndShutdown(t *testing.T) {
	w := &OnlineWorker{done: make(chan struct{})}
	initial := w.accountDisplay.begin(true)
	w.accountDisplay.finish(initial, true, signedInDisplay(), true)
	failed := w.accountDisplay.begin(true)
	w.accountDisplay.finish(failed, true, signedInDisplay(), false)
	if got := w.PlatformSessionSnapshot(); got.DisplayName != "" {
		t.Fatal("failed account transition retained a username", got)
	}
	expired := signedInDisplay()
	past := time.Now().Add(-time.Second)
	expired.ExpiresAt = &past
	token := w.accountDisplay.begin(false)
	w.accountDisplay.finish(token, false, expired, true)
	if got := w.PlatformSessionSnapshot(); got.DisplayName != "" {
		t.Fatal("expired account snapshot retained a username", got)
	}
	token = w.accountDisplay.begin(false)
	w.accountDisplay.finish(token, false, signedInDisplay(), true)
	w.accountDisplay.invalidateOnError(context.Canceled)
	if got := w.PlatformSessionSnapshot(); got.DisplayName == "" {
		t.Fatal("ordinary cancellation discarded valid account display state")
	}
	close(w.done)
	if got := w.PlatformSessionSnapshot(); got.DisplayName != "" {
		t.Fatal("stopped worker retained a username", got)
	}
}
