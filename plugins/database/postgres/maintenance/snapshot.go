package maintenance

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/state"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugindb"
)

func (s *store) snapshot(ctx context.Context, path string) (err error) {
	source, err := s.native.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return err
	}
	defer source.Rollback()
	if err = writeDatabase(ctx, path, func(out *sql.Tx) error {
		return copyMain(ctx, source, out)
	}); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(path)
		}
	}()
	statePath := filepath.Join(filepath.Dir(path), "state", filepath.FromSlash(plugindb.PortableStatePath))
	if err = os.MkdirAll(filepath.Dir(statePath), 0700); err != nil {
		return err
	}
	if err = writeDatabase(ctx, statePath, func(out *sql.Tx) error {
		return copyPluginState(ctx, source, out)
	}); err != nil {
		return err
	}
	err = source.Commit()
	if err != nil {
		os.Remove(statePath)
	}
	return err
}

// 两个输出库均使用独占创建和回滚日志，关闭后每个文件就是完整快照。
func writeDatabase(ctx context.Context, path string, write func(*sql.Tx) error) (err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(path)
		}
	}()
	if err = f.Close(); err != nil {
		return err
	}
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p, RawQuery: "mode=rw"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := db.Close(); err == nil {
			err = closeErr
		}
	}()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := write(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func copyPluginState(ctx context.Context, src, dst *sql.Tx) error {
	if _, err := dst.ExecContext(ctx, state.CreateSchema); err != nil {
		return err
	}
	var initialized bool
	if err := src.QueryRowContext(ctx, `SELECT to_regclass('musicfs_plugin_values') IS NOT NULL`).Scan(&initialized); err != nil || !initialized {
		return err
	}
	rows, err := src.QueryContext(ctx, `SELECT plugin,instance,key,revision,value FROM musicfs_plugin_values`)
	if err != nil {
		return err
	}
	defer rows.Close()
	insert, err := dst.PrepareContext(ctx, `INSERT INTO plugin_values(scope,key,revision,value) VALUES(?,?,?,?)`)
	if err != nil {
		return err
	}
	defer insert.Close()
	for rows.Next() {
		var plugin, instance, key string
		var revision int64
		var value []byte
		if err := rows.Scan(&plugin, &instance, &key, &revision, &value); err != nil {
			return err
		}
		if err := plugindb.ValidateScope(plugin, instance); err != nil {
			return err
		}
		if _, err := insert.ExecContext(ctx, plugin+"\x00"+instance, key, revision, value); err != nil {
			return err
		}
	}
	return rows.Err()
}
