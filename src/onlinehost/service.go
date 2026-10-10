// Package onlinehost scopes public metadata providers without importing audio
// assets or granting account credentials to clients.
package onlinehost

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

type Provider interface {
	Search(context.Context, online.SearchRequest) (online.SearchPage, error)
	Metadata(context.Context, string) (online.Track, error)
	ObservePlaylist(context.Context, string) (online.PlaylistObservation, error)
	PublicRecommendations(context.Context, int) (online.RecommendationPage, error)
}

type Runtime struct {
	Owner, InstanceID, Name, Platform, ConfigRevision string
	Enabled                                           bool
	Provider                                          Provider
	Gate                                              *plugincontrol.Gate
}

type Instance struct {
	InstanceID   string `json:"instanceId"`
	Name         string `json:"name"`
	PluginID     string `json:"pluginId"`
	AccountID    string `json:"accountId"`
	Availability string `json:"availability"`
	pluginproto.OnlineDescriptor
}

type Service struct {
	mu       sync.RWMutex
	runtimes []Runtime
	slots    chan struct{}
}

func New(runtimes []Runtime) (*Service, error) {
	if len(runtimes) > 16 {
		return nil, domain.Fail("INVALID_ARGUMENT", "At most 16 online instances are supported")
	}
	seen := map[string]bool{}
	for _, r := range runtimes {
		key := r.Owner + "\x00" + r.InstanceID
		if !pluginproto.OnlineIdentity(r.Owner) || !pluginproto.OnlineIdentity(r.InstanceID) || pluginproto.OnlinePluginID(r.Platform) == "" || r.ConfigRevision == "" || seen[key] {
			return nil, domain.Fail("INVALID_ARGUMENT", "Invalid online runtime identity")
		}
		seen[key] = true
	}
	return &Service{runtimes: append([]Runtime{}, runtimes...), slots: make(chan struct{}, 4)}, nil
}

func (s *Service) Instances(ctx context.Context, owner string) ([]Instance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := []Instance{}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, r := range s.runtimes {
		if r.Owner != owner {
			continue
		}
		availability := "offline"
		if ready(r) && r.Gate.Check(ctx) == nil {
			availability = "available"
		}
		accountID := ""
		if availability == "available" {
			if view, err := platformSession(ctx, r); err == nil {
				accountID = view.AccountID
			} else {
				availability = "loginRequired"
			}
		}
		out = append(out, Instance{InstanceID: r.InstanceID, Name: r.Name, PluginID: pluginproto.OnlinePluginID(r.Platform), AccountID: accountID, Availability: availability, OnlineDescriptor: pluginproto.DescribeOnline(r.Platform)})
	}
	return out, nil
}

// Runtime is an internal owner/account check for later explicit imports. It
// never changes a binding or substitutes another user's available instance.
func (s *Service) Runtime(ctx context.Context, owner, id, account string) (Runtime, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.runtime(ctx, owner, id, account)
}

func (s *Service) runtime(ctx context.Context, owner, id, account string) (Runtime, error) {
	if err := ctx.Err(); err != nil {
		return Runtime{}, err
	}
	for _, r := range s.runtimes {
		if r.Owner != owner || r.InstanceID != id {
			continue
		}
		if err := r.Gate.Check(ctx); err != nil {
			return Runtime{}, err
		}
		if !ready(r) {
			return Runtime{}, domain.Fail("SOURCE_OFFLINE", "Online instance is unavailable")
		}
		if account != "" {
			view, err := platformSession(ctx, r)
			if err != nil {
				return Runtime{}, err
			}
			if view.AccountID != account {
				return Runtime{}, domain.Fail("ACCESS_DENIED", "Platform account does not match the binding")
			}
		}
		return r, nil
	}
	return Runtime{}, domain.Fail("NOT_FOUND", "Online instance not found")
}

func ready(r Runtime) bool {
	if !r.Enabled || r.Provider == nil || (reflect.ValueOf(r.Provider).Kind() == reflect.Pointer && reflect.ValueOf(r.Provider).IsNil()) {
		return false
	}
	if p, ok := r.Provider.(interface{ Done() <-chan struct{} }); ok {
		select {
		case <-p.Done():
			return false
		default:
		}
	}
	return true
}

func callControl[T any](ctx context.Context, s *Service, owner, id string, f func(context.Context, Runtime) (T, error)) (T, error) {
	var empty T
	ctx, r, release, err := s.AcquireRuntime(ctx, owner, id)
	if err != nil {
		return empty, err
	}
	defer release()
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		seconds := 1
		return empty, &domain.Error{Code: "RATE_LIMITED", Message: "Online request capacity is exhausted", RetryAfter: &seconds}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return f(ctx, r)
}
