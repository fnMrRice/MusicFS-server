// Package maintenance 将 PostgreSQL 的主库和插件状态导出为便携 SQLite 备份。
package maintenance

import (
	"context"
	"database/sql"
	"errors"

	"github.com/fnMrRice/MusicFS-server/plugins/database/postgres/schema"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

type store struct {
	*sqlite.Store
	native *sql.DB
}

// Open 只检查已有结构。成功后由维护连接管理两个连接池的生命周期。
func Open(ctx context.Context, native, queries *sql.DB) (storage.Maintenance, error) {
	if err := schema.CheckVersion(ctx, native); err != nil {
		return nil, err
	}
	return &store{Store: sqlite.NewRepository(queries, "musicfs.plugin.storage.postgres"), native: native}, nil
}

func (s *store) Close() error {
	return errors.Join(s.Store.Close(), s.native.Close())
}

func (s *store) DatabaseSize(ctx context.Context) (int64, error) {
	var size int64
	err := s.native.QueryRowContext(ctx, `SELECT COALESCE(sum(pg_total_relation_size(c.oid)), 0)::bigint
FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
WHERE n.nspname=current_schema() AND c.relkind='r'`).Scan(&size)
	return size, err
}

func (s *store) CreateMediaBackup(ctx context.Context, id, path string) (mediastore.Backup, error) {
	return s.Store.CreateMediaBackupWithSnapshot(ctx, id, path, s.snapshot)
}
