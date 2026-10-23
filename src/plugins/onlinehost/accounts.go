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
