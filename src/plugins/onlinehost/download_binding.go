package onlinehost

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

// DownloadBinding 只记录实例配置与已验证的平台账号身份，不保存平台凭据。
type DownloadBinding struct {
	AccountID, Revision, ConfigRevision, LoginBinding string
}

func downloadBinding(ctx context.Context, r Runtime) (DownloadBinding, error) {
	account, revision, binding, err := verifiedLoginScope(ctx, r)
	return DownloadBinding{AccountID: account, Revision: revision, ConfigRevision: r.ConfigRevision, LoginBinding: binding}, err
}

func (s *Service) DownloadBinding(ctx context.Context, owner, id string) (DownloadBinding, error) {
	r, err := s.Runtime(ctx, owner, id, "")
	if err != nil {
		return DownloadBinding{}, err
	}
	return downloadBinding(ctx, r)
}

func (b DownloadBinding) check(ctx context.Context, r Runtime) error {
	now, err := downloadBinding(ctx, r)
	if err == nil && b != now {
		return domain.Fail("RESOURCE_CHANGED", "Online instance or platform account changed")
	}
	return err
}

func (s *Service) ResolveDownload(ctx context.Context, owner, id, external string, binding DownloadBinding) (library.Track, error) {
	return s.resolveTrack(ctx, owner, id, external, &binding)
}
