package onlinehost

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

func platformSession(ctx context.Context, r Runtime) (online.AccountView, error) {
	var view online.AccountView
	var err error
	if p, ok := r.Provider.(interface {
		PlatformSession(context.Context) (online.AccountView, error)
	}); ok {
		view, err = p.PlatformSession(ctx)
	} else if p, ok := r.Provider.(accountProvider); ok {
		view, err = p.PlatformAccount(ctx, online.AccountRequest{Action: "state"})
	}
	if err != nil {
		return view, err
	}
	if view.State != "signedIn" || view.AccountID == "" || view.Revision == "" {
		return online.AccountView{}, domain.Fail("PLATFORM_AUTH_REQUIRED", "Sign in to the music platform before using this source")
	}
	return view, nil
}

func call[T any](ctx context.Context, s *Service, owner, id string, f func(context.Context, Runtime) (T, error)) (T, error) {
	return callControl(ctx, s, owner, id, func(ctx context.Context, r Runtime) (T, error) {
		var empty T
		before, err := platformSession(ctx, r)
		if err != nil {
			return empty, err
		}
		value, err := f(ctx, r)
		if err != nil {
			return empty, err
		}
		after, err := platformSession(ctx, r)
		if err != nil {
			return empty, err
		}
		if before.AccountID != after.AccountID || before.Revision != after.Revision {
			return empty, domain.Fail("RESOURCE_CHANGED", "Platform account changed during the request")
		}
		return value, nil
	})
}
