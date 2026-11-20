package onlinehost

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

func (s *Service) AttachGate(owner, id string, gate *plugincontrol.Gate) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, r := range s.runtimes {
		if r.Owner == owner && r.InstanceID == id {
			s.runtimes[i].Gate = gate
			return nil
		}
	}
	return domain.Fail("NOT_FOUND", "Online runtime not found")
}
func (s *Service) UpdateRuntime(r Runtime) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, old := range s.runtimes {
		if old.Owner != r.Owner || old.InstanceID != r.InstanceID {
			continue
		}
		if r.Platform != old.Platform || r.ConfigRevision == "" {
			return domain.Fail("INVALID_ARGUMENT", "Online runtime identity cannot change")
		}
		if err := captureDescriptor(&r, old.descriptor); err != nil {
			return err
		}
		r.Gate = old.Gate
		s.runtimes[i] = r
		return nil
	}
	return domain.Fail("NOT_FOUND", "Online runtime not found")
}
func (s *Service) AcquireRuntime(ctx context.Context, owner, id string) (context.Context, Runtime, func(), error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, err := s.runtime(ctx, owner, id, "")
	if err != nil {
		return ctx, Runtime{}, nil, err
	}
	lease, err := r.Gate.Acquire(ctx)
	if err != nil {
		return ctx, Runtime{}, nil, err
	}
	if lease == nil {
		return ctx, r, func() {}, nil
	}
	return lease.Context(), r, lease.Close, nil
}

// RecommendationProvider always resolves the current generation under the host
// lock. The recommendation cache never owns or closes a worker process.
func (s *Service) RecommendationProvider(owner, id string) interface {
	PublicRecommendations(context.Context, int) (online.RecommendationPage, error)
} {
	return scopedRecommendation{host: s, owner: owner, id: id}
}

type scopedRecommendation struct {
	host      *Service
	owner, id string
}

// 推荐适配器同步传递当前实例的已验证声明，不自行重建平台策略。
func (p scopedRecommendation) Descriptor() pluginproto.OnlineDescriptor {
	p.host.mu.RLock()
	defer p.host.mu.RUnlock()
	for _, r := range p.host.runtimes {
		if r.Owner == p.owner && r.InstanceID == p.id {
			return r.descriptor.Clone()
		}
	}
	return pluginproto.OnlineDescriptor{}
}

func (p scopedRecommendation) PublicRecommendations(ctx context.Context, limit int) (online.RecommendationPage, error) {
	return p.host.PublicRecommendations(ctx, p.owner, p.id, limit)
}

func (p scopedRecommendation) CheckAvailability(ctx context.Context) error {
	_, _, err := p.AccountScope(ctx)
	return err
}

// AccountScope binds cached recommendations to a validated login generation.
func (p scopedRecommendation) AccountScope(ctx context.Context) (string, string, error) {
	account, revision, _, err := p.RecommendationAccountScope(ctx)
	return account, revision, err
}

func (p scopedRecommendation) RecommendationAccountScope(ctx context.Context) (string, string, string, error) {
	r, err := p.host.Runtime(ctx, p.owner, p.id, "")
	if err != nil {
		return "", "", "", err
	}
	if r.descriptor.Authentication == "anonymous" {
		return "", "", "", nil
	}
	if provider, ok := r.Provider.(interface {
		RecommendationAccountScope(context.Context) (string, string, string, error)
	}); ok {
		return provider.RecommendationAccountScope(ctx)
	}
	view, err := platformSession(ctx, r)
	if err != nil {
		return "", "", "", err
	}
	return (online.AccountResult{View: view}).RecommendationScope()
}
