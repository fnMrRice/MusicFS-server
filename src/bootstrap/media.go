package bootstrap

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

func configureMedia(ctx context.Context, c config.Config, store storage.Backend) (*mediastore.Files, *mediastore.Service, func(), error) {
	files, err := mediastore.OpenFiles(c.MediaRoot)
	if err != nil {
		return nil, nil, nil, err
	}
	media, err := mediastore.New(store, files)
	if err == nil {
		err = media.Recover(ctx)
	}
	if err != nil {
		files.Close()
		return nil, nil, nil, err
	}
	collector, err := mediastore.NewCollector(store, files, 0)
	if err != nil {
		files.Close()
		return nil, nil, nil, err
	}
	collector.Start(ctx)
	return files, media, func() { collector.Close(); files.Close() }, nil
}
