package onlinehost

import (
	"context"
	"errors"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

type accountProvider interface {
	PlatformAccount(context.Context, online.AccountRequest) (online.AccountView, error)
	PlatformDeveloperToken() string
}
type AccountOptions struct {
	Method         string `json:"method"`
	DeveloperToken string `json:"developerToken,omitempty"`
	Configured     bool   `json:"configured"`
	Validation     string `json:"validation"`
}

func (s *Service) AccountOptions(ctx context.Context, owner, id string) (AccountOptions, error) {
	return callControl(ctx, s, owner, id, func(_ context.Context, r Runtime) (AccountOptions, error) {
		p, ok := r.Provider.(accountProvider)
		if !ok {
			return AccountOptions{}, domain.Fail("UNSUPPORTED", "Platform login is not supported by this worker")
		}
		method := "qr"
		token := ""
		configured := true
		if r.Platform == online.AppleMusic {
			method = "musicKit"
			token = p.PlatformDeveloperToken()
			configured = token != ""
		}
		return AccountOptions{Method: method, DeveloperToken: token, Configured: configured, Validation: "liveAccountPending"}, nil
	})
}
func (s *Service) Account(ctx context.Context, owner, id string, request online.AccountRequest) (online.AccountView, error) {
	return callControl(ctx, s, owner, id, func(ctx context.Context, r Runtime) (online.AccountView, error) {
		p, ok := r.Provider.(accountProvider)
		if !ok {
			return online.AccountView{}, domain.Fail("UNSUPPORTED", "Platform login is not supported by this worker")
		}
		view, err := p.PlatformAccount(ctx, request)
		var failure *domain.Error
		if errors.As(err, &failure) && failure.Code == "AUTH_REQUIRED" {
			err = domain.Fail("PLATFORM_AUTH_REQUIRED", "Platform authorization or developer configuration is required")
		}
		return view, err
	})
}
