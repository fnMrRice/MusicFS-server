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
	if view.State != "signedIn" || view.AccountID == "" || view.Revision == "" || (view.ExpiresAt != nil && !time.Now().Before(*view.ExpiresAt)) {
		return ""
	}
	return view.DisplayName
}

func (s *Service) runtimeInstance(ctx context.Context, runtime Runtime) Instance {
	ctx, current, release, err := s.AcquireRuntime(ctx, runtime.Owner, runtime.InstanceID)
	if err == nil {
		defer release()
		runtime = current
	}
	pluginID := pluginproto.OnlinePluginID(runtime.Platform)
	name := runtimeAlias(runtime)
	availability := "offline"
	accountID, accountRevision := "", ""
	if err == nil {
		if view, err := platformSession(ctx, runtime); err == nil {
			availability = "available"
			accountID, accountRevision = view.AccountID, view.Revision
			if view.DisplayName != "" {
				name = view.DisplayName
			}
		} else {
			var failure *domain.Error
			if errors.As(err, &failure) && (failure.Code == "PLATFORM_AUTH_REQUIRED" || failure.Code == "AUTH_REQUIRED") {
				availability = "loginRequired"
			}
		}
	}
	return Instance{
		InstanceID: runtime.InstanceID, Name: name, PluginID: pluginID,
		AccountID: accountID, AccountRevision: accountRevision, Availability: availability,
		OnlineDescriptor: runtime.descriptor.Clone(),
	}
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
