package bootstrap

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
)

func configureMedia(ctx context.Context, c config.Config, store *sqlite.Store) (*mediastore.Files, *mediastore.Service, error) {
	files, err := mediastore.OpenFiles(c.MediaRoot)
	if err != nil {
		return nil, nil, err
	}
	media, err := mediastore.New(store, files)
	if err == nil {
		err = media.Recover(ctx)
	}
	if err != nil {
		files.Close()
		return nil, nil, err
	}
	return files, media, nil
}
