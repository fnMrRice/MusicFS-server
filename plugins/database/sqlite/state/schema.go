package state

import (
	"context"
	"database/sql"
	_ "embed"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// CreateSchema 是 SQLite 状态库与便携快照共用的建库脚本。
//
//go:embed 001_plugin_values.sql
var CreateSchema string

//go:embed 001_validate_scopes.sql
var validateScopes string

// 初版没有记录 user_version。确认已有行使用当前身份后，可写连接才登记版本。
// 配置预读发生在获取数据锁之前，必须保持只读。
func checkSchema(ctx context.Context, db *sql.DB, writable bool) error {
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version == 1 {
		return nil
	}
	if version != 0 {
		return domain.Fail("CONFLICT", "Plugin state schema is newer than this server")
	}
	if !writable {
		return checkScopes(ctx, db)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, CreateSchema); err != nil {
		return err
	}
	if err := checkScopes(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func checkScopes(ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) error {
	var obsolete bool
	if err := db.QueryRowContext(ctx, validateScopes).Scan(&obsolete); err != nil {
		return err
	}
	if obsolete {
		return domain.Fail("CONFLICT", "Plugin state contains obsolete identities; complete the UUID upgrade described in docs/plugins/plugin-identity.md before restarting")
	}
	return nil
}
