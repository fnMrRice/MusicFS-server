package sqlite

import (
	"context"
	"math"
	"path/filepath"

	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

var _ storage.Backend = (*Store)(nil)
var _ storage.Maintenance = (*Store)(nil)

func init() {
	if err := storage.Register(Driver()); err != nil {
		panic(err)
	}
}

func Driver() storage.Driver {
	return storage.Driver{
		Name: "sqlite", Version: storage.ContractVersion, PluginVersion: bundledplugin.SQLiteVersion,
		SnapshotName: "db.sqlite", RestoredName: "musicfs.sqlite",
		Open: func(ctx context.Context, options storage.Options) (storage.Backend, error) {
			if err := validateOptions(options); err != nil {
				return nil, err
			}
			store, err := Open(ctx, filepath.Join(options.DataRoot, "musicfs.sqlite"))
			if err != nil {
				return nil, err
			}
			if err = store.MigratePluginInstanceIDs(ctx); err != nil {
				store.Close()
				return nil, err
			}
			return store, nil
		},
		OpenMaintenance: func(ctx context.Context, options storage.Options) (storage.Maintenance, error) {
			if err := validateOptions(options); err != nil {
				return nil, err
			}
			return OpenMaintenance(ctx, filepath.Join(options.DataRoot, "musicfs.sqlite"))
		},
		BackupSchemaVersion: BackupSchemaVersion,
		InspectBackup:       InspectBackup,
	}
}

func validateOptions(options storage.Options) error {
	if len(options.Values) != 0 || options.Executable != "" || len(options.Args) != 0 || options.SHA256 != "" {
		return domain.Fail("INVALID_ARGUMENT", "SQLite storage does not accept backend options or an executable")
	}
	return nil
}

func (s *Store) DatabaseSize(ctx context.Context) (int64, error) {
	var pages, size int64
	if err := s.DB.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); err != nil {
		return 0, err
	}
	if err := s.DB.QueryRowContext(ctx, "PRAGMA page_size").Scan(&size); err != nil {
		return 0, err
	}
	if pages < 0 || size < 1 || pages > math.MaxInt64/size {
		return 0, domain.Fail("RATE_LIMITED", "Database size exceeds the supported limit")
	}
	return pages * size, nil
}
