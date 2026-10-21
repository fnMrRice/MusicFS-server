package pluginhost

import (
	"errors"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/plugindisplay"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

// accountDisplaySnapshot is only a display projection. Authorization continues
// to query the worker; reading a name must not wait for its active network calls.
type accountDisplaySnapshot struct {
	mu             sync.RWMutex
	generation     uint64
	pending        int
	view           online.AccountView
	displayCount   *plugindisplay.Count
	countExpiresAt *time.Time
}

func (s *accountDisplaySnapshot) begin(mutation bool) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mutation {
		s.generation++
		s.pending++
		s.view = online.AccountView{}
		s.displayCount = nil
		s.countExpiresAt = nil
	}
	return s.generation
}

func (s *accountDisplaySnapshot) finish(generation uint64, mutation bool, view online.AccountView, count *plugindisplay.Count, valid bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if mutation {
		s.pending--
	}
	if generation != s.generation || s.pending != 0 || !valid {
		return
	}
	// Never retain login URLs, tokens, or unverified/stale account details.
	s.view = online.AccountView{}
	s.displayCount = count.Clone()
	s.countExpiresAt = nil
	if view.State == "signedIn" && view.AccountID != "" && view.Revision != "" {
		s.view = online.AccountView{State: view.State, AccountID: view.AccountID,
			Revision: view.Revision, DisplayName: view.DisplayName}
		if view.ExpiresAt != nil {
			expiry := *view.ExpiresAt
			s.view.ExpiresAt = &expiry
			s.countExpiresAt = &expiry
		}
	}
}

func (s *accountDisplaySnapshot) invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.generation++
	s.view = online.AccountView{}
	s.displayCount = nil
	s.countExpiresAt = nil
}

func (s *accountDisplaySnapshot) count() *plugindisplay.Count {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.pending != 0 || (s.countExpiresAt != nil && !time.Now().Before(*s.countExpiresAt)) {
		return nil
	}
	return s.displayCount.Clone()
}

func (s *accountDisplaySnapshot) invalidateOnError(err error) {
	var failure *domain.Error
	if errors.As(err, &failure) && (failure.Code == "PLATFORM_AUTH_REQUIRED" || failure.Code == "AUTH_REQUIRED") {
		s.invalidate()
	}
}

func (s *accountDisplaySnapshot) current() online.AccountView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	view := s.view
	if s.pending != 0 || view.State != "signedIn" || (view.ExpiresAt != nil && !time.Now().Before(*view.ExpiresAt)) {
		return online.AccountView{State: "signedOut"}
	}
	if view.ExpiresAt != nil {
		expiry := *view.ExpiresAt
		view.ExpiresAt = &expiry
	}
	return view
}

// PlatformSessionSnapshot returns the last verified public account identity
// without RPC. It is scoped to this worker and must never authorize an operation.
func (w *OnlineWorker) PlatformSessionSnapshot() online.AccountView {
	select {
	case <-w.done:
		return online.AccountView{State: "signedOut"}
	default:
		return w.accountDisplay.current()
	}
}
