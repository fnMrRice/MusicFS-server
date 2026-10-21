package state

import (
	"context"
	"database/sql"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginid"
)

// 整个作用域原子迁移，保留配置、发现快照和工作进程状态的版本及实例边界。
// 同一身份对应多个非空作用域时，即使键没有重叠，也必须先解决冲突。
func migratePluginScopes(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	targets, err := readScopeTargets(ctx, tx)
	if err != nil {
		return err
	}
	for current, old := range targets {
		if current == old {
			continue
		}
		if _, err := tx.ExecContext(ctx, `UPDATE plugin_values SET scope=? WHERE scope=?`, current, old); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func readScopeTargets(ctx context.Context, tx *sql.Tx) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT scope FROM plugin_values`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	targets := map[string]string{}
	for rows.Next() {
		var scope string
		if err := rows.Scan(&scope); err != nil {
			return nil, err
		}
		plugin, instance, found := strings.Cut(scope, "\x00")
		target := scope
		if found {
			target = pluginid.Canonical(plugin) + "\x00" + instanceid.Canonical(instance)
		}
		if previous, found := targets[target]; found && previous != scope {
			return nil, domain.Fail("CONFLICT", "Plugin database scopes conflict after plugin identity migration; reconcile the legacy and current scopes before restarting")
		}
		targets[target] = scope
	}
	return targets, rows.Err()
}
