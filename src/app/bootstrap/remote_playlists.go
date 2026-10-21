package bootstrap

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
)

func configureRemotePlaylists(ctx context.Context, repository playlists.Repository, online *onlinehost.Service) (*playlists.Service, error) {
	service, err := playlists.New(repository, online.PlaylistRemote(), 1)
	if err != nil {
		return nil, err
	}
	return service, nil
}
