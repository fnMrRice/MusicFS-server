package migrations

import (
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

// Migration 表示一个原始数据库版本，也包括合并文件中的独立分段。
type Migration struct {
	Version int
	SQL     string
}

// Load 按原始版本顺序返回嵌入的数据库迁移。
func Load() ([]Migration, error) { return LoadFrom(Files) }

// LoadFrom 也供发布工具读取磁盘上的仓库迁移文件。
func LoadFrom(files fs.FS) ([]Migration, error) {
	names, err := fs.Glob(files, "*.sql")
	if err != nil {
		return nil, err
	}
	var result []Migration
	for _, name := range names {
		version, err := fileVersion(name)
		if err != nil {
			return nil, err
		}
		raw, err := fs.ReadFile(files, name)
		if err != nil {
			return nil, err
		}
		body := strings.ReplaceAll(string(raw), "\r\n", "\n")
		sections := strings.Split("\n"+body, "\n-- migration: ")
		batch := []Migration{{Version: version, SQL: body}}
		if len(sections) > 1 {
			if strings.TrimSpace(sections[0]) != "" {
				return nil, fmt.Errorf("SQL precedes the first migration marker in %s", name)
			}
			batch = nil
			for i, section := range sections[1:] {
				header, sql, ok := strings.Cut(section, "\n")
				v, err := fileVersion(strings.TrimSpace(header))
				if !ok || err != nil {
					return nil, fmt.Errorf("invalid migration marker in %s", name)
				}
				if i < len(sections)-2 {
					sql += "\n"
				}
				batch = append(batch, Migration{Version: v, SQL: sql})
			}
			if batch[len(batch)-1].Version != version {
				return nil, fmt.Errorf("migration batch end does not match %s", name)
			}
		}
		for i, migration := range batch {
			if strings.TrimSpace(migration.SQL) == "" {
				return nil, fmt.Errorf("migration %d in %s is empty", migration.Version, name)
			}
			if i > 0 && migration.Version != batch[i-1].Version+1 {
				return nil, fmt.Errorf("migration batch sequence is incomplete in %s", name)
			}
		}
		result = append(result, batch...)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("database migration sequence is empty")
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Version < result[j].Version })
	for i, migration := range result {
		if migration.Version != i+1 {
			return nil, fmt.Errorf("migration sequence is incomplete at version %d", i+1)
		}
	}
	return result, nil
}

func fileVersion(name string) (int, error) {
	prefix, _, ok := strings.Cut(name, "_")
	version, err := strconv.Atoi(prefix)
	if !ok || err != nil || version < 1 || !strings.HasSuffix(name, ".sql") {
		return 0, fmt.Errorf("invalid migration filename %q", name)
	}
	return version, nil
}
