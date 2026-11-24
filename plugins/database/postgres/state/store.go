// Package state 提供 PostgreSQL 插件实例的私有 JSON 状态。
package state

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugindb"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginid"
)

//go:embed 001_plugin_values.sql
var createTable string

var keyPattern = regexp.MustCompile(`^[a-z0-9_-]{1,80}$`)

type store struct {
	db       *sql.DB
	plugin   string
	instance string
}

func scoped(db *sql.DB, plugin, instance string) (*store, error) {
	if plugin == "" || instance == "" {
		return nil, domain.Fail("ACCESS_DENIED", "Database scope is required")
	}
	return &store{db: db, plugin: pluginid.Canonical(plugin), instance: instanceid.Canonical(instance)}, nil
}

type querier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func exists(ctx context.Context, q querier) (bool, error) {
	var found bool
	err := q.QueryRowContext(ctx, "SELECT to_regclass('musicfs_plugin_values') IS NOT NULL").Scan(&found)
	return found, err
}

// Open 成功时接管 db 的所有权。只在第一次可写打开时初始化实例状态表。
func Open(ctx context.Context, db *sql.DB, plugin, instance string) (plugindb.Backend, error) {
	s, err := scoped(db, plugin, instance)
	if err != nil {
		return nil, err
	}
	found, err := exists(ctx, db)
	if err != nil {
		return nil, err
	}
	if found {
		return s, nil
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(1978594171, 2)"); err != nil {
		return nil, err
	}
	if found, err = exists(ctx, tx); err != nil {
		return nil, err
	}
	if !found {
		if _, err = tx.ExecContext(ctx, createTable); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return s, nil
}

// Read 在获取服务数据锁之前使用。首次部署时，尚未创建状态表等价于空状态。
func Read(ctx context.Context, db *sql.DB, plugin, instance, key string) (pluginproto.StateValue, error) {
	s, err := scoped(db, plugin, instance)
	if err != nil {
		return pluginproto.StateValue{}, err
	}
	if !keyPattern.MatchString(key) {
		return pluginproto.StateValue{}, domain.Fail("INVALID_ARGUMENT", "Invalid database key")
	}
	found, err := exists(ctx, db)
	if err != nil || !found {
		return pluginproto.StateValue{}, err
	}
	return s.get(ctx, key)
}

func (s *store) Get(ctx context.Context, key string) (pluginproto.StateValue, error) {
	if !keyPattern.MatchString(key) {
		return pluginproto.StateValue{}, domain.Fail("INVALID_ARGUMENT", "Invalid database key")
	}
	return s.get(ctx, key)
}

func (s *store) get(ctx context.Context, key string) (pluginproto.StateValue, error) {
	var v pluginproto.StateValue
	err := s.db.QueryRowContext(ctx,
		"SELECT revision,value FROM musicfs_plugin_values WHERE plugin=$1 AND instance=$2 AND key=$3",
		s.plugin, s.instance, key).Scan(&v.Revision, &v.Value)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return v, err
}

// CompareAndSet 以修订号约束每次写入，JSON null 与其他值一样推进修订。
func (s *store) CompareAndSet(ctx context.Context, r pluginproto.StateRequest) (pluginproto.StateValue, error) {
	if !keyPattern.MatchString(r.Key) || r.Revision < 0 || len(r.Value) > 512<<10 || !json.Valid(r.Value) {
		return pluginproto.StateValue{}, domain.Fail("INVALID_ARGUMENT", "Invalid database value")
	}
	var res sql.Result
	var err error
	if r.Revision == 0 {
		res, err = s.db.ExecContext(ctx, `INSERT INTO musicfs_plugin_values(plugin,instance,key,revision,value)
			VALUES ($1,$2,$3,1,$4) ON CONFLICT DO NOTHING`, s.plugin, s.instance, r.Key, []byte(r.Value))
	} else {
		res, err = s.db.ExecContext(ctx, `UPDATE musicfs_plugin_values SET revision=revision+1,value=$4
			WHERE plugin=$1 AND instance=$2 AND key=$3 AND revision=$5`, s.plugin, s.instance, r.Key, []byte(r.Value), r.Revision)
	}
	if err != nil {
		return pluginproto.StateValue{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return pluginproto.StateValue{}, err
	}
	if n != 1 {
		return pluginproto.StateValue{}, domain.Fail("PRECONDITION_FAILED", "Database revision changed")
	}
	return pluginproto.StateValue{Revision: r.Revision + 1, Value: r.Value}, nil
}

func (s *store) Close() error { return s.db.Close() }
