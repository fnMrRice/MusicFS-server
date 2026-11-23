package onlinehost

import (
	"context"
	"errors"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type accountProvider interface {
	PlatformAccount(context.Context, online.AccountRequest) (online.AccountView, error)
}
type AccountOptions = online.AccountOptions

// OnPlaylistAccountReady 在启动时登记回调，账号重新可用后继续补齐歌单。
func (s *Service) OnPlaylistAccountReady(f func(context.Context, Runtime, string)) {
	s.playlistLogins = make(map[string][2]string)
	s.playlistAccountReady = f
}

func (s *Service) AccountOptions(ctx context.Context, owner, id string) (AccountOptions, error) {
	return callControl(ctx, s, owner, id, func(ctx context.Context, r Runtime) (AccountOptions, error) {
		if r.descriptor.Login == nil {
			return AccountOptions{Method: "none", Configured: false, Validation: "notSupported"}, nil
		}
		p, ok := r.Provider.(interface {
			PlatformAccountOptions(context.Context) (online.AccountOptions, error)
		})
		if !ok {
			return AccountOptions{}, domain.Fail("UNSUPPORTED", "Platform login is not supported by this worker")
		}
		options, err := p.PlatformAccountOptions(ctx)
		if err == nil {
			err = options.Validate(r.descriptor.Login)
		}
		return options, err
	})
}
func (s *Service) Account(ctx context.Context, owner, id string, request online.AccountRequest) (online.AccountView, error) {
	return callControl(ctx, s, owner, id, func(ctx context.Context, r Runtime) (online.AccountView, error) {
		if request.Action != "state" {
			s.endAudioAccount(id)
			defer s.endAudioAccount(id)
		}
		p, ok := r.Provider.(accountProvider)
		if !ok {
			return online.AccountView{}, domain.Fail("UNSUPPORTED", "Platform login is not supported by this worker")
		}
		view, err := p.PlatformAccount(ctx, request)
		if err == nil && view.State == "signedIn" && view.AccountID != "" && view.Revision != "" && s.playlistAccountReady != nil &&
			(request.Action == "complete" || request.Action == "poll") {
			s.mu.Lock()
			generation := [2]string{view.AccountID, view.Revision}
			changed := s.playlistLogins[id] != generation
			s.playlistLogins[id] = generation
			s.mu.Unlock()
			if changed {
				s.playlistAccountReady(ctx, r, view.AccountID)
			}
		}
		var failure *domain.Error
		if errors.As(err, &failure) && failure.Code == "AUTH_REQUIRED" {
			err = domain.Fail("PLATFORM_AUTH_REQUIRED", "Platform authorization or developer configuration is required")
		}
		return view, err
	})
}

func (s *Service) endAudioAccount(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if changed := s.audioAccounts[id]; changed != nil {
		close(changed)
		delete(s.audioAccounts, id)
	}
}

func (s *Service) watchAudioAccount(ctx context.Context, r Runtime) (online.AccountView, <-chan struct{}, error) {
	s.mu.Lock()
	changed := s.audioAccounts[r.InstanceID]
	if changed == nil {
		changed = make(chan struct{})
		s.audioAccounts[r.InstanceID] = changed
	}
	s.mu.Unlock()
	view, err := platformSession(ctx, r)
	return view, changed, err
}
