// Package state 提供 SQLite 插件按实例隔离的 JSON 私有状态存储。
package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginid"
	_ "modernc.org/sqlite"
)

var keyPattern = regexp.MustCompile(`^[a-z0-9_-]{1,80}$`)

type Store struct {
	db    *sql.DB
	scope string
}

// CreateValuesTable 是实例状态库和便携快照共用的表结构。
const CreateValuesTable = `CREATE TABLE IF NOT EXISTS plugin_values(scope TEXT NOT NULL, key TEXT NOT NULL, revision INTEGER NOT NULL, value BLOB NOT NULL, PRIMARY KEY(scope,key))`

func Open(ctx context.Context, path, plugin, instance string) (store *Store, openErr error) {
	ctx = pluginlog.Context(ctx, "musicfs.plugin.storage.sqlite", instance)
	finish := pluginlog.Operation(ctx, "database.state.open")
	defer func() { finish(openErr) }()
	if plugin == "" || instance == "" {
		return nil, domain.Fail("ACCESS_DENIED", "Database scope is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p}
	db, err := sql.Open("sqlite", u.String()+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.ExecContext(ctx, CreateValuesTable)
	if err != nil {
		db.Close()
		return nil, err
	}
	if err := migratePluginScopes(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, scope: pluginid.Canonical(plugin) + "\x00" + instanceid.Canonical(instance)}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Get(ctx context.Context, key string) (value pluginproto.StateValue, getErr error) {
	ctx = pluginlog.Context(ctx, "musicfs.plugin.storage.sqlite", "")
	finish := pluginlog.Operation(ctx, "database.state.get")
	defer func() { finish(getErr) }()
	var v pluginproto.StateValue
	if !keyPattern.MatchString(key) {
		return v, domain.Fail("INVALID_ARGUMENT", "Invalid database key")
	}
	err := s.db.QueryRowContext(ctx, `SELECT revision,value FROM plugin_values WHERE scope=? AND key=?`, s.scope, key).Scan(&v.Revision, &v.Value)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return v, err
}

// CompareAndSet 在多个连接间保持原子性。null 作为值保存而不删除键，
// 防止通过删除并重新创建键来重置版本。
func (s *Store) CompareAndSet(ctx context.Context, r pluginproto.StateRequest) (value pluginproto.StateValue, saveErr error) {
	ctx = pluginlog.Context(ctx, "musicfs.plugin.storage.sqlite", "")
	finish := pluginlog.Operation(ctx, "database.state.compareAndSet")
	defer func() { finish(saveErr) }()
	if !keyPattern.MatchString(r.Key) || r.Revision < 0 || len(r.Value) > 512<<10 || !json.Valid(r.Value) {
		return pluginproto.StateValue{}, domain.Fail("INVALID_ARGUMENT", "Invalid database value")
	}
	var result sql.Result
	var err error
	if r.Revision == 0 {
		result, err = s.db.ExecContext(ctx, `INSERT INTO plugin_values(scope,key,revision,value) VALUES(?,?,1,?) ON CONFLICT DO NOTHING`, s.scope, r.Key, []byte(r.Value))
	} else {
		result, err = s.db.ExecContext(ctx, `UPDATE plugin_values SET revision=revision+1,value=? WHERE scope=? AND key=? AND revision=?`, []byte(r.Value), s.scope, r.Key, r.Revision)
	}
	if err != nil {
		return pluginproto.StateValue{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return pluginproto.StateValue{}, err
	}
	if n != 1 {
		return pluginproto.StateValue{}, domain.Fail("PRECONDITION_FAILED", "Database revision changed")
	}
	return pluginproto.StateValue{Revision: r.Revision + 1, Value: r.Value}, nil
}
