package playback

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// DeleteOnlineDownload 依据 MusicFS 用户和来源校验归属，允许在平台账号退出后清理自己的下载。
func (s *Service) DeleteOnlineDownload(ctx context.Context, owner, sourceID, versionID string) error {
	if s.downloads == nil {
		return domain.Fail("NOT_FOUND", "Online download not found")
	}
	return s.downloads.Delete(ctx, owner, sourceID, versionID)
}
