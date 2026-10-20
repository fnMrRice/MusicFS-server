package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/storage/migrations"
	_ "modernc.org/sqlite"
)

type Store struct{ DB *sql.DB }

func Open(ctx context.Context, path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, errors.New("cannot create database directory")
	}
	uriPath := filepath.ToSlash(path)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	db, err := sql.Open("sqlite", u.String()+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{DB: db}
	if err = s.migrate(ctx, path); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}
func (s *Store) Close() error { return s.DB.Close() }
func (s *Store) migrate(ctx context.Context, path string) error {
	if _, err := s.DB.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations(version INTEGER PRIMARY KEY)"); err != nil {
		return fmt.Errorf("cannot initialize database: %w", err)
	}
	var current int
	s.DB.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM schema_migrations").Scan(&current)
	entries, err := migrations.Files.ReadDir(".")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	latest := 0
	for _, entry := range entries {
		v, _ := strconv.Atoi(strings.Split(entry.Name(), "_")[0])
		if v > latest {
			latest = v
		}
	}
	if current > latest {
		return errors.New("database schema is newer than this server")
	}
	for _, entry := range entries {
		v, _ := strconv.Atoi(strings.Split(entry.Name(), "_")[0])
		if v <= current {
			continue
		}
		if current > 0 {
			backup := fmt.Sprintf("%s.before-%03d", path, v)
			if _, err := os.Stat(backup); os.IsNotExist(err) {
				if err := s.Backup(ctx, backup); err != nil {
					return err
				}
			}
		}
		body, err := migrations.Files.ReadFile(entry.Name())
		if err != nil {
			return err
		}
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, string(body)); err == nil {
			_, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations(version) VALUES (?)", v)
		}
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("database migration %d failed", v)
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		current = v
	}
	return nil
}
func (s *Store) Backup(ctx context.Context, dest string) error {
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		return errors.New("backup destination must not exist")
	}
	_, err := s.DB.ExecContext(ctx, "VACUUM INTO '"+strings.ReplaceAll(dest, "'", "''")+"'")
	return err
}
