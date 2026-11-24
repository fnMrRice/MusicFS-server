// Package postgres 实现 MusicFS 的原生 PostgreSQL 存储后端。
package postgres

import (
	"context"
	"database/sql"
	"errors"

	"github.com/fnMrRice/MusicFS-server/plugins/database/postgres/dialect"
	"github.com/fnMrRice/MusicFS-server/plugins/database/postgres/maintenance"
	"github.com/fnMrRice/MusicFS-server/plugins/database/postgres/schema"
	"github.com/fnMrRice/MusicFS-server/plugins/database/postgres/state"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugindb"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

func init() {
	if err := storage.Register(Driver()); err != nil {
		panic(err)
	}
}

var errDatabaseInUse = errors.New("PostgreSQL database is already in use by MusicFS")

func Driver() storage.Driver {
	return storage.Driver{
		Name: "postgres", Version: storage.ContractVersion, PluginVersion: bundledplugin.PostgresVersion,
		SnapshotName: "db.sqlite", RestoredName: "musicfs.sqlite", SeparatePluginStateSnapshot: true,
		BackupSchemaVersion: sqlite.BackupSchemaVersion, InspectBackup: sqlite.InspectBackup,
		Open: open, OpenMaintenance: openMaintenance, OpenPluginState: openState, ReadPluginState: readState,
	}
}

// 单个运行实例拥有主库写入权。每次物理重连都重新取锁，备份同样独占此库。
func lockDatabase(ctx context.Context, conn *pgx.Conn) error {
	var acquired bool
	err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(1296454483, 1)").Scan(&acquired)
	if err != nil {
		_ = conn.Close(ctx)
		return err
	}
	if !acquired {
		_ = conn.Close(ctx)
		return errDatabaseInUse
	}
	return nil
}

func openPools(ctx context.Context, options storage.Options) (*sql.DB, *sql.DB, error) {
	config, err := connectionConfig(options.Values)
	if err != nil {
		return nil, nil, err
	}
	connector := stdlib.GetConnector(*config, stdlib.OptionAfterConnect(lockDatabase))
	queries := sql.OpenDB(dialect.Wrap(connector))
	queries.SetMaxOpenConns(1)
	if err = queries.PingContext(ctx); err != nil {
		queries.Close()
		var pgError *pgconn.PgError
		switch {
		case ctx.Err() != nil:
			return nil, nil, ctx.Err()
		case errors.Is(err, errDatabaseInUse):
			return nil, nil, errDatabaseInUse
		case errors.As(err, &pgError):
			return nil, nil, errors.New("PostgreSQL rejected the connection (SQLSTATE " + pgError.Code + ")")
		default:
			return nil, nil, errors.New("cannot connect to PostgreSQL (check host, port and TLS settings)")
		}
	}
	return stdlib.OpenDB(*config), queries, nil
}

func open(ctx context.Context, options storage.Options) (storage.Backend, error) {
	native, queries, err := openPools(ctx, options)
	if err != nil {
		return nil, err
	}
	defer native.Close()
	if err := schema.Migrate(ctx, native); err != nil {
		queries.Close()
		return nil, err
	}
	// 对宿主只暴露运行期仓储，离线维护由独立工厂处理。
	return struct{ storage.Backend }{sqlite.NewRepository(queries, "musicfs.plugin.storage.postgres")}, nil
}

func openMaintenance(ctx context.Context, options storage.Options) (storage.Maintenance, error) {
	native, queries, err := openPools(ctx, options)
	if err != nil {
		return nil, err
	}
	store, err := maintenance.Open(ctx, native, queries)
	if err != nil {
		native.Close()
		queries.Close()
		return nil, err
	}
	return store, nil
}

func openState(ctx context.Context, cfg plugindb.BackendConfig, _, plugin, instance string) (plugindb.Backend, error) {
	config, err := connectionConfig(cfg.Options)
	if err != nil {
		return nil, err
	}
	db := stdlib.OpenDB(*config)
	store, err := state.Open(ctx, db, plugin, instance)
	if err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func readState(ctx context.Context, cfg plugindb.BackendConfig, _, plugin, instance, key string) (pluginproto.StateValue, error) {
	config, err := connectionConfig(cfg.Options)
	if err != nil {
		return pluginproto.StateValue{}, err
	}
	db := stdlib.OpenDB(*config)
	defer db.Close()
	return state.Read(ctx, db, plugin, instance, key)
}
