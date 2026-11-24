// Package dialect 将共享仓储的查询绑定到 PostgreSQL 连接。
package dialect

import (
	"context"
	"database/sql/driver"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
)

// Wrap 接受 pgx stdlib 连接器，仅用于业务仓储。迁移、备份和维护使用 PostgreSQL 原生连接。
func Wrap(base driver.Connector) driver.Connector { return connector{base} }

type connector struct{ driver.Connector }

func (c connector) Connect(ctx context.Context) (driver.Conn, error) {
	raw, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	pg, ok := raw.(*stdlib.Conn)
	if !ok {
		raw.Close()
		return nil, errors.New("PostgreSQL repository requires a pgx connection")
	}
	return &connection{Conn: pg, queries: make(map[string]string)}, nil
}

type connection struct {
	*stdlib.Conn
	queries map[string]string
}

// database/sql 串行调用同一物理连接。重复的业务语句仅编译一次；动态批量查询占用的缓存有界。
func (c *connection) query(sql string) string {
	if bound, ok := c.queries[sql]; ok {
		return bound
	}
	bound := translate(sql)
	if len(c.queries) < 1024 && len(sql)+len(bound) <= 16*1024 {
		c.queries[sql] = bound
	}
	return bound
}

func (c *connection) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *connection) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	return c.Conn.PrepareContext(ctx, c.query(query))
}

func (c *connection) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.ExecContext(ctx, c.query(query), args)
}

func (c *connection) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.QueryContext(ctx, c.query(query), args)
}

func (c *connection) CheckNamedValue(arg *driver.NamedValue) error {
	value, err := driver.DefaultParameterConverter.ConvertValue(arg.Value)
	if err != nil {
		return err
	}
	switch v := value.(type) {
	case bool:
		// 两个后端均以整数保存标志，查询中的开关参数也遵循相同约定。
		value = int64(0)
		if v {
			value = int64(1)
		}
	case time.Time:
		value = v.UTC().Format(time.RFC3339Nano)
	}
	arg.Value = value
	return nil
}
