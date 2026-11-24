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

	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugindb"
	_ "modernc.org/sqlite"
)

var keyPattern = regexp.MustCompile(`^[a-z0-9_-]{1,80}$`)

type Store struct {
	db    *sql.DB
	scope string
}

func Open(ctx context.Context, path, plugin, instance string) (store *Store, openErr error) {
	ctx = pluginlog.Context(ctx, "musicfs.plugin.storage.sqlite", instance)
	finish := pluginlog.Operation(ctx, "database.state.open")
	defer func() { finish(openErr) }()
	store, err := scoped(plugin, instance)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := openDatabase(path, "_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	if err := checkSchema(ctx, db, true); err != nil {
		db.Close()
		return nil, err
	}
	store.db = db
	return store, nil
}

func scoped(plugin, instance string) (*Store, error) {
	if err := plugindb.ValidateScope(plugin, instance); err != nil {
		return nil, err
	}
	return &Store{scope: plugin + "\x00" + instance}, nil
}

func openDatabase(path, options string) (*sql.DB, error) {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p}
	db, err := sql.Open("sqlite", u.String()+"?_pragma=busy_timeout(5000)&"+options)
	if err == nil {
		db.SetMaxOpenConns(1)
	}
	return db, err
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
	return s.get(ctx, key)
}

func (s *Store) get(ctx context.Context, key string) (pluginproto.StateValue, error) {
	var v pluginproto.StateValue
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
