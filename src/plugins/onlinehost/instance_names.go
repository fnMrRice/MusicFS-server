package onlinehost

import (
	"context"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

// InstanceName resolves only the requesting owner's current account name.
// Signed-out or offline instances use the plugin-owned display alias.
func (s *Service) InstanceName(ctx context.Context, owner, id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, runtime := range s.runtimes {
		if runtime.Owner == owner && runtime.InstanceID == id {
			if name := runtimeAccountName(ctx, runtime); name != "" {
				return name
			}
			return runtimeAlias(runtime)
		}
	}
	return ""
}

// AccountName exposes only a current, verified public name for this owner.
// It never falls back to a plugin alias or performs a network request.
func (s *Service) AccountName(ctx context.Context, owner, id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, runtime := range s.runtimes {
		if runtime.Owner == owner && runtime.InstanceID == id {
			return runtimeAccountName(ctx, runtime)
		}
	}
	return ""
}

func runtimeAccountName(ctx context.Context, runtime Runtime) string {
	if !ready(runtime) || runtime.Gate.Check(ctx) != nil {
		return ""
	}
	provider, ok := runtime.Provider.(interface {
		PlatformSessionSnapshot() online.AccountView
	})
	if !ok {
		return ""
	}
	view := provider.PlatformSessionSnapshot()
	if !view.VerifiedAt(time.Now()) {
		return ""
	}
	return view.DisplayName
}

func (s *Service) runtimeInstance(ctx context.Context, runtime Runtime) Instance {
	runtime, availability, view := s.displayStatus(ctx, runtime)
	name := runtimeAlias(runtime)
	if view.DisplayName != "" {
		name = view.DisplayName
	}
	return Instance{
		InstanceID: runtime.InstanceID, Name: name, PluginID: pluginproto.OnlinePluginID(runtime.Platform),
		AccountID: view.AccountID, AccountRevision: view.Revision, Availability: availability,
		OnlineDescriptor: runtime.descriptor.Clone(),
	}
}

// 两个来源列表共用进程和账号的展示状态。读音频和平台写入仍需实时授权。
func (s *Service) displayStatus(ctx context.Context, runtime Runtime) (Runtime, string, online.AccountView) {
	var empty online.AccountView
	ctx, current, release, err := s.AcquireRuntime(ctx, runtime.Owner, runtime.InstanceID)
	if err != nil {
		return runtime, "offline", empty
	}
	defer release()
	if current.descriptor.Authentication == "anonymous" {
		return current, "available", empty
	}
	var view online.AccountView
	switch p := current.Provider.(type) {
	case interface {
		PlatformDisplaySession(context.Context) (online.AccountView, error)
	}:
		view, err = p.PlatformDisplaySession(ctx)
	case interface{ PlatformSessionSnapshot() online.AccountView }:
		view = p.PlatformSessionSnapshot()
	default:
		view, err = platformSession(ctx, current)
	}
	if err != nil {
		var failure *domain.Error
		if errors.As(err, &failure) && (failure.Code == "PLATFORM_AUTH_REQUIRED" || failure.Code == "AUTH_REQUIRED") {
			return current, "loginRequired", empty
		}
		return current, "offline", empty
	}
	if !view.VerifiedAt(time.Now()) {
		return current, "loginRequired", empty
	}
	return current, "available", view
}

func runtimeAlias(runtime Runtime) string {
	pluginID := pluginproto.OnlinePluginID(runtime.Platform)
	identity := runtime.descriptor.Plugin
	if identity == nil {
		identity = pluginconfig.Identity(pluginID)
	}
	name := ""
	if identity != nil {
		name = identity.Name
	}
	return name
}
