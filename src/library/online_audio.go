package library

import (
	"context"
	"io"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// AudioRepository 只发布在线曲目元数据，不将元数据摘要当作固定字节版本。
type AudioRepository interface {
	PublishOnlineTrack(context.Context, string, string, online.Track) (Track, error)
}

// AddAudioRuntime 注册在线来源；实际读取始终重新确认当前实例权限。
func (s *Service) AddAudioRuntime(r Runtime) error {
	if r.Owner == "" || r.ID == "" || r.SourceType != "network" || r.OpenAudio == nil || r.DescribeAudio == nil || r.AuthorizeAudio == nil {
		return domain.Fail("INVALID_ARGUMENT", "Invalid online audio runtime")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.runtimes[r.ID]; exists {
		return domain.Fail("CONFLICT", "Provider identity is already registered")
	}
	s.runtimes[r.ID] = r
	s.gates[r.ID] = &providerGate{barriers: map[string]bool{}, tasks: map[string]*ReadLease{}, changed: make(chan struct{})}
	return nil
}

func (s *Service) SourceType(id string) string {
	r, ok := s.Runtime(id)
	if !ok {
		return ""
	}
	return r.SourceType
}

func (s *Service) DescribeNetworkAudio(ctx context.Context, t Track) (pluginproto.OnlineAudio, error) {
	r, ok := s.Runtime(t.ProviderID)
	if !ok || r.Owner != t.Owner || r.DescribeAudio == nil {
		return pluginproto.OnlineAudio{}, domain.Fail("NOT_FOUND", "Network audio source not found")
	}
	if _, err := r.AuthorizeAudio(ctx); err != nil {
		return pluginproto.OnlineAudio{}, err
	}
	return r.DescribeAudio(ctx, t.ExternalID)
}

func (s *Service) RegisterOnlineTrack(ctx context.Context, owner, id string, metadata online.Track) (Track, error) {
	r, ok := s.Runtime(id)
	if !ok || r.Owner != owner || r.OpenAudio == nil {
		return Track{}, domain.Fail("NOT_FOUND", "Online audio source not found")
	}
	repo, ok := s.repo.(AudioRepository)
	if !ok {
		return Track{}, domain.Fail("UNSUPPORTED", "Storage does not support online audio")
	}
	if _, err := r.AuthorizeAudio(ctx); err != nil {
		return Track{}, err
	}
	return repo.PublishOnlineTrack(ctx, owner, id, metadata)
}

// OpenOnlineAudio 仅由播放流消费时调用，不参与元数据建档或会话创建。
func (s *Service) OpenOnlineAudio(ctx context.Context, t Track) (pluginproto.OnlineAudio, io.ReadCloser, error) {
	r, ok := s.Runtime(t.ProviderID)
	if !ok || r.Owner != t.Owner || r.OpenAudio == nil {
		return pluginproto.OnlineAudio{}, nil, domain.Fail("NOT_FOUND", "Online audio source not found")
	}
	if _, err := r.AuthorizeAudio(ctx); err != nil {
		return pluginproto.OnlineAudio{}, nil, err
	}
	return r.OpenAudio(ctx, t.ExternalID)
}
