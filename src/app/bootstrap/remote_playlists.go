package bootstrap

import (
	"context"
	"log/slog"

	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
)

func configureRemotePlaylists(repository playlists.Repository, online *onlinehost.Service, runtimes []onlinehost.Runtime) (*playlists.Service, error) {
	prepare := func(ctx context.Context, r onlinehost.Runtime, account string) {
		if err := repository.PreparePlaylistCatalog(ctx, r.Owner, r.InstanceID, r.Platform, r.ConfigRevision, account); err != nil {
			slog.WarnContext(ctx, "Unable to resume playlist catalog sync", "instanceId", r.InstanceID, "errorCode", playlists.FailureCode(err))
		}
	}
	online.OnPlaylistAccountReady(prepare)
	service, err := playlists.New(repository, online.PlaylistRemote(), 1)
	if err != nil {
		return nil, err
	}
	service.OnQueueIdle(func(ctx context.Context) {
		for _, configured := range runtimes {
			// 空账号只查询数据库中尚未安排的导入；检查运行时就绪无需访问平台账号。
			r, err := online.Runtime(ctx, configured.Owner, configured.InstanceID, "")
			if err == nil {
				prepare(ctx, r, "")
			}
		}
	})
	return service, nil
}
