// 此工具编译共享仓储中可静态求值的 Go 字符串查询，不执行其中的业务读写。
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/postgres/dialect"
	"github.com/fnMrRice/MusicFS-server/plugins/database/postgres/schema"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) (err error) {
	dsn := os.Getenv("MUSICFS_TEST_POSTGRES_DSN")
	if dsn == "" {
		return errors.New("set MUSICFS_TEST_POSTGRES_DSN to a PostgreSQL test database")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return errors.New("invalid MUSICFS_TEST_POSTGRES_DSN")
	}
	queries, dynamic, err := catalog()
	if err != nil {
		return err
	}
	if config.ConnectTimeout == 0 {
		config.ConnectTimeout = 10 * time.Second
	}
	admin := stdlib.OpenDB(*config)
	defer admin.Close()
	name := "musicfs_query_check_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{name}.Sanitize()
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+identifier); err != nil {
		return fmt.Errorf("cannot create test schema: %w", err)
	}
	defer func() {
		// 即使检查被取消也要回收本次独占的结构。
		cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, cleanupErr := admin.ExecContext(cleanup, "DROP SCHEMA "+identifier+" CASCADE"); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("cannot remove test schema %s: %w", name, cleanupErr))
		}
	}()
	isolated := config.Copy()
	isolated.RuntimeParams["search_path"] = name
	native := stdlib.OpenDB(*isolated)
	defer native.Close()
	if err := schema.Migrate(ctx, native); err != nil {
		return err
	}
	db := sql.OpenDB(dialect.Wrap(stdlib.GetConnector(*isolated)))
	defer db.Close()
	invalid := 0
	for _, q := range queries {
		stmt, err := db.PrepareContext(ctx, q.text)
		if err != nil {
			var detail *pgconn.PgError
			if !errors.As(err, &detail) || ctx.Err() != nil {
				return fmt.Errorf("preparing %s: %w", q.location, err)
			}
			fmt.Fprintf(os.Stderr, "%s: [%s] %s\n%s\n", q.location, detail.Code, detail.Message, q.text)
			invalid++
			continue
		}
		if err := stmt.Close(); err != nil {
			return err
		}
	}
	fmt.Printf("Checked %d distinct static queries, %d invalid. Skipped %d dynamic query call sites.\n", len(queries), invalid, dynamic)
	if invalid != 0 {
		return errors.New("PostgreSQL query check failed")
	}
	return nil
}

type query struct {
	text, location string
}

// 只收集两个引擎共用的运行期查询。SQLite 的物理建库、旧数据升级和便携快照校验另行测试。
func sharedFile(name string) bool {
	switch name {
	case "store.go", "backend.go", "media_backup_snapshot.go":
		return false
	}
	return !strings.HasSuffix(name, "_test.go") && !strings.HasPrefix(name, "instance_migration")
}

func catalog() ([]query, int, error) {
	names, err := filepath.Glob("plugins/database/sqlite/*.go")
	if err != nil || len(names) == 0 {
		return nil, 0, errors.New("run the query check from the Server repository root")
	}
	files := token.NewFileSet()
	var trees []*ast.File
	constants := make(map[string]ast.Expr)
	for _, name := range names {
		if !sharedFile(filepath.Base(name)) {
			continue
		}
		tree, err := parser.ParseFile(files, name, nil, 0)
		if err != nil {
			return nil, 0, err
		}
		trees = append(trees, tree)
		for _, decl := range tree.Decls {
			group, ok := decl.(*ast.GenDecl)
			if !ok || group.Tok != token.CONST {
				continue
			}
			for _, spec := range group.Specs {
				value := spec.(*ast.ValueSpec)
				for i, id := range value.Names {
					if i < len(value.Values) {
						constants[id.Name] = value.Values[i]
					}
				}
			}
		}
	}
	var queries []query
	seen := make(map[string]bool)
	dynamic := 0
	for _, tree := range trees {
		ast.Inspect(tree, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			expression := queryArgument(call)
			if expression == nil {
				return true
			}
			q, ok := constantText(expression, constants)
			if !ok {
				dynamic++
			} else if !seen[q] {
				seen[q] = true
				queries = append(queries, query{q, files.Position(call.Pos()).String()})
			}
			return true
		})
	}
	if len(queries) == 0 {
		return nil, dynamic, errors.New("no static repository queries found")
	}
	return queries, dynamic, nil
}

func queryArgument(call *ast.CallExpr) ast.Expr {
	index := -1
	switch f := call.Fun.(type) {
	case *ast.SelectorExpr:
		switch f.Sel.Name {
		case "ExecContext", "QueryContext", "QueryRowContext", "PrepareContext":
			index = 1
		case "Exec", "Query", "QueryRow", "Prepare":
			index = 0
		}
	case *ast.Ident:
		switch f.Name {
		case "musicRows", "readAlbumIDs":
			index = 2
		}
	}
	if index >= 0 && index < len(call.Args) {
		return call.Args[index]
	}
	return nil
}

// 变量的取值依赖运行时分支。只展开字符串常量、括号和静态字符串连接。
func constantText(expr ast.Expr, constants map[string]ast.Expr) (string, bool) {
	switch value := expr.(type) {
	case *ast.BasicLit:
		if value.Kind == token.STRING {
			s, err := strconv.Unquote(value.Value)
			return s, err == nil
		}
	case *ast.ParenExpr:
		return constantText(value.X, constants)
	case *ast.Ident:
		if value.Obj != nil {
			if value.Obj.Kind != ast.Con {
				return "", false
			}
			if spec, ok := value.Obj.Decl.(*ast.ValueSpec); ok {
				for i, id := range spec.Names {
					if id.Name == value.Name && i < len(spec.Values) {
						return constantText(spec.Values[i], constants)
					}
				}
			}
			return "", false
		}
		if source, ok := constants[value.Name]; ok {
			return constantText(source, constants)
		}
	case *ast.BinaryExpr:
		if value.Op == token.ADD {
			lhs, left := constantText(value.X, constants)
			rhs, right := constantText(value.Y, constants)
			return lhs + rhs, left && right
		}
	}
	return "", false
}
