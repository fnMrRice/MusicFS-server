package testkit

import (
	"context"
	"database/sql"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// PostgresOptions 创建本用例专属的空库。测试账号须能创建数据库。
func PostgresOptions(t *testing.T) map[string]string {
	t.Helper()
	return postgresOptions(t, "")
}

// PostgresICUOptions 让排序测试在数据库默认区域规则与 SQLite 不同的环境下运行。
func PostgresICUOptions(t *testing.T) map[string]string {
	t.Helper()
	return postgresOptions(t, " LOCALE_PROVIDER icu ICU_LOCALE 'en-US'")
}

func postgresOptions(t *testing.T, locale string) map[string]string {
	t.Helper()
	dsn := os.Getenv("MUSICFS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("MUSICFS_TEST_POSTGRES_DSN is not configured")
	}
	address, err := url.Parse(dsn)
	if err != nil || (address.Scheme != "postgres" && address.Scheme != "postgresql") {
		t.Fatal("test PostgreSQL DSN must be a PostgreSQL URL")
	}
	adminConfig, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test PostgreSQL DSN")
	}
	admin := stdlib.OpenDB(*adminConfig)
	t.Cleanup(func() { admin.Close() })
	name := "musicfs_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err = admin.ExecContext(t.Context(), "CREATE DATABASE "+name+" TEMPLATE template0"+locale); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		// 并行删除多个测试库会等待 PostgreSQL 检查点完成。
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if _, err := admin.ExecContext(ctx, "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("remove test database: %v", err)
		}
	})
	port := address.Port()
	if port == "" {
		port = "5432"
	}
	mode := address.Query().Get("sslmode")
	if mode == "" {
		mode = "verify-full"
	}
	options := map[string]string{"host": adminConfig.Host, "port": port, "database": name, "user": adminConfig.User, "sslMode": mode}
	if cert := address.Query().Get("sslrootcert"); cert != "" {
		options["sslRootCert"] = cert
	}
	if password := adminConfig.Password; password != "" {
		path := filepath.Join(t.TempDir(), "password")
		if err := os.WriteFile(path, []byte(password), 0600); err != nil {
			t.Fatal(err)
		}
		options["passwordFile"] = path
	}
	return options
}

// PostgresDatabase 为数据库级断言打开原生连接，与 Server 使用同一测试库。
func PostgresDatabase(t *testing.T, options map[string]string) *sql.DB {
	t.Helper()
	address := url.URL{Scheme: "postgres", User: url.User(options["user"]), Host: net.JoinHostPort(options["host"], options["port"]), Path: "/" + options["database"]}
	query := url.Values{"sslmode": {options["sslMode"]}}
	if cert := options["sslRootCert"]; cert != "" {
		query.Set("sslrootcert", cert)
	}
	address.RawQuery = strings.ReplaceAll(query.Encode(), "+", "%20")
	config, err := pgx.ParseConfig(address.String())
	if err != nil {
		t.Fatal("invalid test PostgreSQL configuration")
	}
	if file := options["passwordFile"]; file != "" {
		password, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		config.Password = string(password)
	}
	db := stdlib.OpenDB(*config)
	t.Cleanup(func() { db.Close() })
	return db
}
