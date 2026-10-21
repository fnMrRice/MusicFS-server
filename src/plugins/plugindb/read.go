package plugindb

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginid"
)

// Read resolves legacy scopes without migrating or creating a database. Initial
// configuration loading runs before the server data lock and must not change
// scopes that another, possibly older, server is still using.
func Read(ctx context.Context, path, plugin, instance, key string) (pluginproto.StateValue, error) {
	var value pluginproto.StateValue
	if plugin == "" || instance == "" {
		return value, domain.Fail("ACCESS_DENIED", "Database scope is required")
	}
	if !keyPattern.MatchString(key) {
		return value, domain.Fail("INVALID_ARGUMENT", "Invalid database key")
	}
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p}
	db, err := sql.Open("sqlite", u.String()+"?mode=ro&_pragma=query_only(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return value, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return value, err
	}
	defer tx.Rollback()
	targets, err := readScopeTargets(ctx, tx)
	if err != nil {
		return value, err
	}
	scope := pluginid.Canonical(plugin) + "\x00" + instanceid.Canonical(instance)
	if old, exists := targets[scope]; exists {
		scope = old
	}
	err = tx.QueryRowContext(ctx, `SELECT revision,value FROM plugin_values WHERE scope=? AND key=?`, scope, key).Scan(&value.Revision, &value.Value)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return value, err
}
