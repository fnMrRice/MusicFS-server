package maintenance

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
)

func copyMain(ctx context.Context, src, dst *sql.Tx) error {
	steps, err := migrations.Load()
	if err != nil {
		return err
	}
	if _, err := dst.ExecContext(ctx, `CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	for _, step := range steps {
		if _, err := dst.ExecContext(ctx, step.SQL); err != nil {
			return fmt.Errorf("cannot create portable schema version %d: %w", step.Version, err)
		}
		if _, err := dst.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES(?)`, step.Version); err != nil {
			return err
		}
	}
	// 导入基础表时暂时移除派生写入触发器，避免重复生成关联记录。
	triggers, err := objects(ctx, dst, `SELECT name, sql FROM sqlite_schema WHERE type='trigger' ORDER BY rowid`)
	if err != nil {
		return err
	}
	for _, trigger := range triggers {
		if _, err := dst.ExecContext(ctx, "DROP TRIGGER "+quote(trigger.name)); err != nil {
			return err
		}
	}
	tables, err := objects(ctx, dst, `SELECT name,sql FROM sqlite_schema
WHERE type='table' AND name NOT GLOB 'sqlite_*' AND name!='schema_migrations' ORDER BY name`)
	if err != nil {
		return err
	}
	for _, table := range tables {
		if err := copyTable(ctx, src, dst, table); err != nil {
			return fmt.Errorf("cannot export table %s: %w", table.name, err)
		}
	}
	for _, trigger := range triggers {
		if _, err := dst.ExecContext(ctx, trigger.ddl); err != nil {
			return err
		}
	}
	return nil
}

type object struct{ name, ddl string }

func objects(ctx context.Context, db *sql.Tx, query string, args ...any) ([]object, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []object
	for rows.Next() {
		var o object
		if err := rows.Scan(&o.name, &o.ddl); err != nil {
			return nil, err
		}
		result = append(result, o)
	}
	return result, rows.Err()
}

func quote(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }

func copyTable(ctx context.Context, src, dst *sql.Tx, table object) error {
	name := quote(table.name)
	columns, err := objects(ctx, dst, `SELECT name, type FROM pragma_table_info(?) ORDER BY cid`, table.name)
	if err != nil {
		return err
	}
	// 这两张表将 rowid 作为公开分页游标或恢复顺序，恢复后必须保留原值。
	if table.name == "asset_evidence" || table.name == "config_operations" {
		columns = append(columns, object{"rowid", "INTEGER"})
	}
	names := make([]string, len(columns))
	for i, c := range columns {
		names[i] = quote(c.name)
	}
	fields := strings.Join(names, ",")
	if _, err := dst.ExecContext(ctx, "DELETE FROM "+name); err != nil {
		return err
	}
	insert, err := dst.PrepareContext(ctx, "INSERT INTO "+name+"("+fields+") VALUES("+strings.TrimSuffix(strings.Repeat("?,", len(columns)), ",")+")")
	if err != nil {
		return err
	}
	defer insert.Close()
	rows, err := src.QueryContext(ctx, "SELECT "+fields+" FROM "+name)
	if err != nil {
		return err
	}
	defer rows.Close()
	values, dest := make([]any, len(columns)), make([]any, len(columns))
	for i := range values {
		dest[i] = &values[i]
	}
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return err
		}
		for i, col := range columns {
			if value, ok := values[i].(string); ok && strings.EqualFold(col.ddl, "BLOB") {
				values[i] = []byte(value)
			}
		}
		if _, err := insert.ExecContext(ctx, values...); err != nil {
			return err
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if strings.Contains(strings.ToUpper(table.ddl), "AUTOINCREMENT") {
		return copySequence(ctx, src, dst, table.name)
	}
	return nil
}

// 即使最后几条记录已删除，恢复后也不能重用已对外返回的持久化序号。
func copySequence(ctx context.Context, src, dst *sql.Tx, table string) error {
	var key string
	if err := dst.QueryRowContext(ctx, `SELECT name FROM pragma_table_info(?) WHERE pk=1`, table).Scan(&key); err != nil {
		return err
	}
	var high sql.NullInt64
	if err := src.QueryRowContext(ctx, `SELECT pg_sequence_last_value(pg_get_serial_sequence($1,$2)::regclass)`, table, key).Scan(&high); err != nil {
		return err
	}
	if !high.Valid {
		return nil
	}
	result, err := dst.ExecContext(ctx, `UPDATE sqlite_sequence SET seq=max(seq,?) WHERE name=?`, high.Int64, table)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		_, err = dst.ExecContext(ctx, `INSERT INTO sqlite_sequence(name,seq) VALUES(?,?)`, table, high.Int64)
	}
	return err
}
