package playback

import (
	"context"
	"io"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
)

type downloadAccountKey struct{}
type downloadAccount struct{ id, revision string }

// WithDownloadAccount 绑定排队时的真实平台账号及登录代次；匿名实例使用两个空值。
func WithDownloadAccount(ctx context.Context, accountID, revision string) context.Context {
	return context.WithValue(ctx, downloadAccountKey{}, downloadAccount{accountID, revision})
}

// DownloadOnlineTrack 将本次选定音质交给来源解析，并只在实际版本完整入库后返回。
func (s *Service) DownloadOnlineTrack(ctx context.Context, owner, sourceID, quality string) (onlinecache.Version, error) {
	var empty onlinecache.Version
	if owner == "" || sourceID == "" || len(sourceID) > 128 {
		return empty, domain.Fail("INVALID_ARGUMENT", "Invalid online source identity")
	}
	if s.downloads == nil {
		return empty, domain.Fail("UNSUPPORTED", "Online downloads are unavailable")
	}
	s.mu.Lock()
	if s.ctx.Err() != nil {
		s.mu.Unlock()
		return empty, domain.Fail("TEMPORARY_FAILURE", "Server is draining")
	}
	s.downloadTasks.Add(1)
	s.mu.Unlock()
	defer s.downloadTasks.Done()
	ctx, cancel := context.WithCancel(ctx)
	unlink := context.AfterFunc(s.ctx, cancel)
	defer func() { unlink(); cancel() }()
	track, err := s.library.Track(ctx, owner, sourceID)
	if err != nil {
		return empty, err
	}
	if s.library.SourceType(track.ProviderID) != pluginproto.SourceNetwork {
		return empty, domain.Fail("UNSUPPORTED", "Only online sources can be downloaded")
	}
	if track.Availability != "available" {
		return empty, domain.Fail("SOURCE_OFFLINE", "Track is unavailable")
	}
	if err = (pluginproto.OnlineAudioReference{ExternalID: track.ExternalID, Quality: quality}).Validate(); err != nil {
		return empty, err
	}

	lease, err := s.library.HoldRead(ctx, owner, track.ProviderID)
	if err != nil {
		return empty, err
	}
	defer lease.Close()
	run, cancelRead := context.WithCancel(provider.WithPlaybackQuality(lease.Context(), quality))
	defer cancelRead()
	n, err := s.networkInput(run, track)
	if err != nil {
		return empty, err
	}
	if binding, ok := ctx.Value(downloadAccountKey{}).(downloadAccount); ok && (binding.id != n.scope.Account || binding.revision != n.scope.Revision) {
		return empty, domain.Fail("RESOURCE_CHANGED", "Platform login changed since the download was queued")
	}
	if n.preferences.LimitBytes <= 0 {
		return empty, onlinecache.RetentionDisabled()
	}
	n.watch(run, cancelRead)
	check := func() error { return n.checkAccess(run) }
	if err = check(); err != nil {
		return empty, err
	}
	audio, err := s.library.DescribeNetworkAudio(run, track)
	if invalid := check(); invalid != nil {
		return empty, invalid
	}
	if err != nil {
		return empty, err
	}
	if audio.Validate() != nil || audio.ExternalID != track.ExternalID {
		return empty, domain.Fail("PROTOCOL_MISMATCH", "Invalid online download description")
	}
	open := func(ctx context.Context) (pluginproto.OnlineAudio, io.ReadCloser, error) {
		if err := check(); err != nil {
			return pluginproto.OnlineAudio{}, nil, err
		}
		return s.library.OpenOnlineAudio(ctx, track)
	}
	version, err := s.downloads.Download(run, n.scope, audio, n.preferences.LimitBytes, open, check)
	if invalid := check(); invalid != nil {
		return empty, invalid
	}
	return version, err
}

// DeleteOnlineDownload 依据 MusicFS 用户和来源校验归属，允许在平台账号退出后清理自己的下载。
func (s *Service) DeleteOnlineDownload(ctx context.Context, owner, sourceID, versionID string) error {
	if s.downloads == nil {
		return domain.Fail("NOT_FOUND", "Online download not found")
	}
	return s.downloads.Delete(ctx, owner, sourceID, versionID)
}
