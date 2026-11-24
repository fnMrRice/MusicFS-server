// Package schema 保存并应用 PostgreSQL 原生建库与迁移 SQL。
package schema

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
)

const Version = 2

//go:embed *.sql
var migrations embed.FS

// Migrate 在持有同一数据库的事务锁后检查版本，避免多个实例同时建表。
func Migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(1978594171, 1)"); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS postgres_schema_migrations(version BIGINT PRIMARY KEY)"); err != nil {
		return err
	}
	var current int
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(version),0) FROM postgres_schema_migrations").Scan(&current); err != nil {
		return err
	}
	if current > Version {
		return fmt.Errorf("database schema version %d is newer than supported version %d", current, Version)
	}
	for version := current + 1; version <= Version; version++ {
		files, err := fs.Glob(migrations, fmt.Sprintf("%03d_*.sql", version))
		if err != nil || len(files) == 0 {
			return fmt.Errorf("missing database migration %d", version)
		}
		if version == 1 {
			files = append([]string{"functions.sql"}, files...)
		}
		for _, name := range files {
			statement, err := migrations.ReadFile(name)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, string(statement)); err != nil {
				return fmt.Errorf("applying PostgreSQL migration %s: %w", name, err)
			}
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO postgres_schema_migrations(version) VALUES ($1)", version); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CheckVersion 离线维护仅读取现有结构，不创建或升级数据库。
func CheckVersion(ctx context.Context, db *sql.DB) error {
	var current int
	if err := db.QueryRowContext(ctx, "SELECT coalesce(max(version),0) FROM postgres_schema_migrations").Scan(&current); err != nil {
		return err
	}
	if current != Version {
		return fmt.Errorf("expected database schema version %d, found %d", Version, current)
	}
	return nil
}
