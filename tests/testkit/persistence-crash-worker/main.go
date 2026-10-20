package main

import (
	"context"
	"os"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/src/storage/rpcbackend"
)

type crashBackend struct{ storage.Backend }

func (s *crashBackend) SaveAuth(ctx context.Context, session auth.Session) error {
	if err := s.Backend.SaveAuth(ctx, session); err != nil {
		return err
	}
	os.Exit(23)
	return nil
}

func main() {
	driver := sqlite.Driver()
	open := driver.Open
	driver.Open = func(ctx context.Context, options storage.Options) (storage.Backend, error) {
		store, err := open(ctx, options)
		if err != nil {
			return nil, err
		}
		return &crashBackend{store}, nil
	}
	if rpcbackend.Serve(context.Background(), os.Stdin, os.Stdout, driver) != nil {
		os.Exit(1)
	}
}
