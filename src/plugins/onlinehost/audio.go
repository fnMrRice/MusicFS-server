package onlinehost

import (
	"context"
	"io"
	"sync"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

// ConfigureAudio 将在线元数据及按需读取入口接入公共曲目和播放服务。
func (s *Service) ConfigureAudio(lib *library.Service) error {
	if lib == nil {
		return domain.Fail("INVALID_ARGUMENT", "Invalid online audio library")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.audioLibrary != nil {
		return domain.Fail("CONFLICT", "Online audio library is already configured")
	}
	for _, r := range s.runtimes {
		owner, id := r.Owner, r.InstanceID
		authorize := func(ctx context.Context) (*plugincontrol.Gate, error) {
			current, err := s.Runtime(ctx, owner, id, "")
			if err != nil {
				return nil, err
			}
			if _, err = platformSession(ctx, current); err != nil {
				return nil, err
			}
			return current.Gate, nil
		}
		open := func(ctx context.Context, external string) (pluginproto.OnlineAudio, io.ReadCloser, error) {
			return s.openAudio(ctx, owner, id, external)
		}
		describe := func(ctx context.Context, external string) (pluginproto.OnlineAudio, error) {
			return s.describeAudio(ctx, owner, id, external)
		}
		details := func(ctx context.Context, external string) (pluginproto.TrackDetails, error) {
			return s.TrackDetails(ctx, owner, id, external)
		}
		account := func(ctx context.Context) (online.AccountView, <-chan struct{}, error) {
			current, err := s.Runtime(ctx, owner, id, "")
			if err != nil {
				return online.AccountView{}, nil, err
			}
			if current.descriptor.Authentication == "anonymous" {
				return online.AccountView{}, nil, nil
			}
			if worker, ok := current.Provider.(interface {
				PlatformAudioSession(context.Context) (online.AccountView, <-chan struct{}, error)
			}); ok {
				return worker.PlatformAudioSession(ctx)
			}
			return s.watchAudioAccount(ctx, current)
		}
		status := func(ctx context.Context) (string, string) {
			current, availability, _ := s.displayStatus(ctx, r)
			if current.descriptor.Authentication == "anonymous" {
				return availability, "notRequired"
			}
			switch availability {
			case "available":
				return availability, "authorized"
			case "loginRequired":
				return "unavailable", "authRequired"
			default:
				return availability, "authRequired"
			}
		}
		if err := lib.AddAudioRuntime(library.Runtime{
			ID: id, Owner: owner, Name: r.Name, SourceType: r.descriptor.SourceType,
			OpenAudio: open, DescribeAudio: describe, AuthorizeAudio: authorize, AudioAccount: account,
			OnlineTrackDetails: details, ProviderStatus: status, AudioPluginID: pluginproto.OnlinePluginID(r.Platform), Plugin: r.descriptor.Plugin,
		}); err != nil {
			return err
		}
	}
	s.audioLibrary = lib
	return nil
}

func (s *Service) describeAudio(ctx context.Context, owner, id, external string) (pluginproto.OnlineAudio, error) {
	return call(ctx, s, owner, id, func(ctx context.Context, r Runtime) (pluginproto.OnlineAudio, error) {
		if !supportsAudio(r) {
			return pluginproto.OnlineAudio{}, domain.Fail("UNSUPPORTED", "Network audio is not enabled by this plugin")
		}
		audio, ok := r.Provider.(provider.AudioClient)
		if !ok {
			return pluginproto.OnlineAudio{}, domain.Fail("UNSUPPORTED", "Network audio description is unavailable")
		}
		return audio.DescribeAudio(ctx, external)
	})
}

func (s *Service) Resolve(ctx context.Context, owner, id, external string) (library.Track, error) {
	return s.resolveTrack(ctx, owner, id, external, nil)
}

func (s *Service) resolveTrack(ctx context.Context, owner, id, external string, binding *DownloadBinding) (library.Track, error) {
	if !externalID.MatchString(external) {
		return library.Track{}, domain.Fail("INVALID_ARGUMENT", "Invalid external track identity")
	}
	return call(ctx, s, owner, id, func(ctx context.Context, r Runtime) (library.Track, error) {
		if !supportsAudio(r) {
			return library.Track{}, domain.Fail("UNSUPPORTED", "Network audio is not enabled by this plugin")
		}
		if _, ok := r.Provider.(provider.AudioClient); !ok || s.audioLibrary == nil {
			return library.Track{}, domain.Fail("UNSUPPORTED", "No complete audio resource is available")
		}
		if binding != nil {
			if err := binding.check(ctx, r); err != nil {
				return library.Track{}, err
			}
		}
		metadata, err := r.Provider.Metadata(ctx, external)
		if err != nil {
			return library.Track{}, err
		}
		if binding != nil {
			if err := binding.check(ctx, r); err != nil {
				return library.Track{}, err
			}
		}
		return s.audioLibrary.RegisterOnlineTrack(ctx, owner, id, metadata)
	})
}

func (s *Service) openAudio(ctx context.Context, owner, id, external string) (pluginproto.OnlineAudio, io.ReadCloser, error) {
	ctx, r, release, err := s.AcquireRuntime(ctx, owner, id)
	if err != nil {
		return pluginproto.OnlineAudio{}, nil, err
	}
	if _, err = platformSession(ctx, r); err != nil {
		release()
		return pluginproto.OnlineAudio{}, nil, err
	}
	if !supportsAudio(r) {
		release()
		return pluginproto.OnlineAudio{}, nil, domain.Fail("UNSUPPORTED", "Network audio is not enabled by this plugin")
	}
	audio, ok := r.Provider.(provider.AudioClient)
	if !ok {
		release()
		return pluginproto.OnlineAudio{}, nil, domain.Fail("UNSUPPORTED", "Online audio is unavailable")
	}
	metadata, body, err := audio.OpenAudio(ctx, external)
	if err != nil {
		release()
		return pluginproto.OnlineAudio{}, nil, err
	}
	if body == nil {
		release()
		return pluginproto.OnlineAudio{}, nil, domain.Fail("PROTOCOL_MISMATCH", "Online audio stream is missing")
	}
	return metadata, &audioLeaseReader{ReadCloser: body, release: release}, nil
}

func supportsAudio(r Runtime) bool {
	if r.descriptor.SourceType != pluginproto.SourceNetwork {
		return false
	}
	for _, capability := range r.descriptor.Capabilities {
		if capability.Name == "stream.resolve" {
			return capability.Supported
		}
	}
	return false
}

type audioLeaseReader struct {
	io.ReadCloser
	release func()
	once    sync.Once
}

func (r *audioLeaseReader) Close() error {
	var err error
	r.once.Do(func() { err = r.ReadCloser.Close(); r.release() })
	return err
}
