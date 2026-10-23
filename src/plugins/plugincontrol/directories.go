package plugincontrol

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

const directoryPageSize = 100
const directoryMaxOffset = 1000000

type Directory struct {
	Name             string  `json:"name"`
	Path             string  `json:"path"`
	Available        bool    `json:"available"`
	ConfiguredRootID *string `json:"configuredRootId"`
}

type Directories struct {
	Revision         string      `json:"revision"`
	Path             *string     `json:"path"`
	ParentPath       *string     `json:"parentPath"`
	Items            []Directory `json:"items"`
	NextCursor       *string     `json:"nextCursor"`
	ConfiguredRootID *string     `json:"configuredRootId"`
}

// Directories is an administrator-only deployment view. It works before a local
// provider has selected any music roots or started its worker.
func (m *Manager) Directories(ctx context.Context, id, path, cursor string) (Directories, error) {
	m.mu.Lock()
	r := m.runtimes[id]
	if r == nil {
		m.mu.Unlock()
		return Directories{}, domain.Fail("NOT_FOUND", "Plugin instance not found")
	}
	p, instance, revision := r.plugin, r.instance, m.revision
	m.mu.Unlock()
	if !pluginconfig.SupportsDirectories(p.PluginID) {
		return Directories{}, domain.Fail("UNSUPPORTED", "This plugin does not support music directory selection")
	}
	page := Directories{Revision: revision, Items: []Directory{}}
	scope := id + "\x00" + revision + "\x00"
	if path == "" {
		return allowedDirectoryPage(ctx, instance, scope, cursor, page)
	}
	_, rel, err := config.DirectoryBoundary(instance, path)
	if err != nil {
		return page, err
	}
	root, err := config.OpenAllowedDirectory(instance, path)
	if err != nil {
		return page, err
	}
	defer root.Close()
	file, err := root.Open(".")
	if err != nil {
		return page, directoryUnavailable()
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return page, directoryUnavailable()
	}
	path = filepath.Clean(path)
	page.Path = &path
	configuredID := config.ConfiguredDirectoryIDs(instance)
	page.ConfiguredRootID = configuredID(path)
	if rel != "." {
		parent := filepath.Dir(path)
		page.ParentPath = &parent
	}
	scope += path + "\x00" + strconv.FormatInt(info.ModTime().UnixNano(), 10)
	offset, err := directoryOffset(cursor, scope)
	if err != nil {
		return page, err
	}
	// Both allocation and the amount of skipped work are bounded. File entries
	// count toward a page but are never returned to the administrator.
	for skipped := 0; skipped < offset; {
		if err := ctx.Err(); err != nil {
			return page, err
		}
		n := min(directoryPageSize, offset-skipped)
		entries, err := file.ReadDir(n)
		skipped += len(entries)
		if err != nil || len(entries) == 0 {
			return page, expiredDirectoryCursor()
		}
	}
	entries, err := file.ReadDir(directoryPageSize)
	if err != nil && err != io.EOF {
		return page, directoryUnavailable()
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return page, err
		}
		isDir := entry.IsDir()
		if entry.Type()&os.ModeSymlink != 0 {
			info, err := root.Stat(entry.Name())
			isDir = err == nil && info.IsDir()
		}
		if !isDir {
			continue
		}
		child := filepath.Join(path, entry.Name())
		if config.ValidateDirectoryPath(child) != nil {
			continue
		}
		page.Items = append(page.Items, Directory{Name: entry.Name(), Path: child, Available: readableDirectory(root, entry.Name()), ConfiguredRootID: configuredID(child)})
	}
	if len(entries) == directoryPageSize {
		page.NextCursor = directoryCursor(scope, offset+len(entries))
	}
	return page, nil
}

func allowedDirectoryPage(ctx context.Context, instance config.Instance, scope, cursor string, page Directories) (Directories, error) {
	offset, err := directoryOffset(cursor, scope)
	if err != nil {
		return page, err
	}
	paths := config.AllowedDirectories(instance)
	if offset > len(paths) {
		return page, expiredDirectoryCursor()
	}
	end := min(len(paths), offset+directoryPageSize)
	configuredID := config.ConfiguredDirectoryIDs(instance)
	for _, path := range paths[offset:end] {
		if err := ctx.Err(); err != nil {
			return page, err
		}
		path = filepath.Clean(path)
		available := false
		if config.ValidateDirectoryPath(path) == nil {
			if root, err := os.OpenRoot(path); err == nil {
				available = readableDirectory(root, ".")
				root.Close()
			}
		}
		page.Items = append(page.Items, Directory{Name: filepath.Base(path), Path: path, Available: available, ConfiguredRootID: configuredID(path)})
	}
	if end < len(paths) {
		page.NextCursor = directoryCursor(scope, end)
	}
	return page, nil
}

func readableDirectory(root *os.Root, name string) bool {
	child, err := root.OpenRoot(name)
	if err != nil {
		return false
	}
	defer child.Close()
	file, err := child.Open(".")
	if err != nil {
		return false
	}
	defer file.Close()
	_, err = file.ReadDir(1)
	return err == nil || err == io.EOF
}

func directoryCursor(scope string, offset int) *string {
	value := fmt.Sprintf("%x:%d", sha256.Sum256([]byte(scope)), offset)
	encoded := base64.RawURLEncoding.EncodeToString([]byte(value))
	return &encoded
}

func directoryOffset(cursor, scope string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	if len(cursor) > 256 {
		return 0, invalidDirectoryCursor()
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	parts := strings.Split(string(raw), ":")
	if err != nil || len(parts) != 2 || len(parts[0]) != 64 {
		return 0, invalidDirectoryCursor()
	}
	offset, err := strconv.Atoi(parts[1])
	if err != nil || offset <= 0 || offset > directoryMaxOffset || strconv.Itoa(offset) != parts[1] {
		return 0, invalidDirectoryCursor()
	}
	if parts[0] != fmt.Sprintf("%x", sha256.Sum256([]byte(scope))) {
		return 0, expiredDirectoryCursor()
	}
	return offset, nil
}

func invalidDirectoryCursor() error {
	return domain.Fail("INVALID_ARGUMENT", "Invalid directory cursor")
}

func expiredDirectoryCursor() error {
	return domain.Fail("CURSOR_EXPIRED", "Directory cursor expired; restart directory browsing")
}

func directoryUnavailable() error {
	return domain.Fail("SOURCE_UNAVAILABLE", "Music directory is unavailable")
}
