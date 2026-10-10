package bootstrap

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/onlinehost"
	"github.com/fnMrRice/MusicFS-server/src/playlists"
)

func configureRemotePlaylists(ctx context.Context, repository playlists.Repository, online *onlinehost.Service) (*playlists.Service, error) {
	service, err := playlists.New(repository, online.PlaylistRemote(), 1)
	if err != nil {
		return nil, err
	}
	if err := service.Start(ctx); err != nil {
		return nil, err
	}
	return service, nil
}
