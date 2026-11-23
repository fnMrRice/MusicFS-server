// Package onlinehost 隔离在线实例的元数据、播放描述和按需音频读取，凭据不暴露给客户端。
package onlinehost

import (
	"context"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
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
	descriptor                                        pluginproto.OnlineDescriptor
}

type Instance struct {
	InstanceID      string `json:"instanceId"`
	Name            string `json:"name"`
	PluginID        string `json:"pluginId"`
	AccountID       string `json:"accountId"`
	AccountRevision string `json:"accountRevision,omitempty"`
	Availability    string `json:"availability"`
	pluginproto.OnlineDescriptor
}

type Service struct {
	playlistCache *dailyPlaylistCache
	audioLibrary  *library.Service
	mu            sync.RWMutex
	runtimes      []Runtime
	slots         chan struct{}
}

func New(runtimes []Runtime, options ...func(*Service)) (*Service, error) {
	if len(runtimes) > 16 {
		return nil, domain.Fail("INVALID_ARGUMENT", "At most 16 online instances are supported")
	}
	runtimes = append([]Runtime{}, runtimes...)
	seen := map[string]bool{}
	for i, r := range runtimes {
		key := r.Owner + "\x00" + r.InstanceID
		if !pluginproto.OnlineIdentity(r.Owner) || !pluginproto.OnlineIdentity(r.InstanceID) || pluginproto.OnlinePluginID(r.Platform) == "" || r.ConfigRevision == "" || seen[key] {
			return nil, domain.Fail("INVALID_ARGUMENT", "Invalid online runtime identity")
		}
		seen[key] = true
		if err := captureDescriptor(&r, pluginproto.OnlinePolicy(r.Platform)); err != nil {
			return nil, err
		}
		runtimes[i] = r
	}
	s := &Service{runtimes: runtimes, slots: make(chan struct{}, 4)}
	for _, configure := range options {
		configure(s)
	}
	return s, nil
}

func (s *Service) Instances(ctx context.Context, owner string) ([]Instance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := []Instance{}
	s.mu.RLock()
	runtimes := slices.Clone(s.runtimes)
	s.mu.RUnlock()
	for _, r := range runtimes {
		if r.Owner != owner {
			continue
		}
		out = append(out, s.runtimeInstance(ctx, r))
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
