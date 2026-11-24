package playback

import (
	"context"
	"io"
	"reflect"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

type networkInput struct {
	format         *networkFormat
	audio          pluginproto.OnlineAudio
	profile        OutputProfile
	profilePending bool
	ranges         *onlineRepresentation
	scope          onlinecache.Scope
	preferences    onlinecache.Preferences
	accountChanged <-chan struct{}
	expires        *time.Time
}

func (n networkInput) checkAccess(ctx context.Context) error {
	select {
	case <-n.accountChanged:
		return domain.Fail("RESOURCE_CHANGED", "Platform login changed")
	default:
	}
	if n.expires != nil && !time.Now().Before(*n.expires) {
		return domain.Fail("PLATFORM_AUTH_REQUIRED", "Platform login expired")
	}
	return ctx.Err()
}

func (s *Service) networkInput(ctx context.Context, t library.Track) (networkInput, error) {
	var input networkInput
	r, ok := s.library.Runtime(t.ProviderID)
	if !ok || r.Owner != t.Owner {
		return input, domain.Fail("NOT_FOUND", "Online source not found")
	}
	input.scope = onlinecache.Scope{Owner: t.Owner, Instance: t.ProviderID, Source: t.ID, Plugin: r.AudioPluginID}
	if input.scope.Plugin == "" && r.Plugin != nil {
		input.scope.Plugin = r.Plugin.ID
	}
	if r.AudioAccount != nil {
		view, changed, err := r.AudioAccount(ctx)
		if err != nil {
			return input, err
		}
		if changed != nil && !view.VerifiedAt(time.Now()) {
			return input, domain.Fail("PLATFORM_AUTH_REQUIRED", "Platform login expired")
		}
		input.scope.Account, input.scope.Revision = view.AccountID, view.Revision
		input.accountChanged, input.expires = changed, view.ExpiresAt
	}
	repo, _ := s.repo.(settings.Repository)
	p, err := onlinecache.ReadPreferences(ctx, repo, t.Owner, input.scope.Plugin)
	input.preferences = p
	input.format = &networkFormat{ready: make(chan struct{})}
	return input, err
}

func (n networkInput) watch(ctx context.Context, cancel context.CancelFunc) {
	if n.accountChanged == nil {
		return
	}
	go func() {
		var expiry <-chan time.Time
		if n.expires != nil {
			timer := time.NewTimer(time.Until(*n.expires))
			defer timer.Stop()
			expiry = timer.C
		}
		select {
		case <-ctx.Done():
			return
		case <-n.accountChanged:
			cancel()
		case <-expiry:
			cancel()
		}
	}()
}

func (s *Service) openNetworkReader(ctx context.Context, a *session) (io.ReadCloser, error) {
	n := a.network
	ctx = provider.WithPlaybackQuality(ctx, n.preferences.Quality)
	r, _ := s.library.Runtime(a.track.ProviderID)
	if r.AudioAccount != nil {
		view, _, err := r.AudioAccount(ctx)
		if err != nil {
			return nil, err
		}
		if view.AccountID != n.scope.Account || view.Revision != n.scope.Revision {
			return nil, domain.Fail("RESOURCE_CHANGED", "Platform login changed")
		}
	}
	// 缓存命中也要重新向平台验证播放权、实际音质及当前媒体版本。
	audio, err := s.library.DescribeNetworkAudio(ctx, a.track)
	if err != nil {
		return nil, err
	}
	if audio.Validate() != nil || !reflect.DeepEqual(audio, n.audio) {
		return nil, domain.Fail("RESOURCE_CHANGED", "Online audio representation changed")
	}
	open := func(ctx context.Context) (pluginproto.OnlineAudio, io.ReadCloser, error) {
		return s.library.OpenOnlineAudio(ctx, a.track)
	}
	if s.downloads != nil {
		if n.ranges != nil {
			return s.downloads.OpenSeekable(ctx, n.scope, audio, n.preferences, n.ranges.size, open)
		}
		return s.downloads.Open(ctx, n.scope, audio, n.preferences, open)
	}
	actual, body, err := open(ctx)
	if err == nil && !onlinecache.Matches(n.scope, audio, actual) {
		body.Close()
		return nil, domain.Fail("RESOURCE_CHANGED", "Online audio representation changed")
	}
	return body, err
}
