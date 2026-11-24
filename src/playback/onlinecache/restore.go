package onlinecache

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

var partialFile = regexp.MustCompile(`^partial-[0-9]+$`)

func (m *Manager) restoreDirectories(ctx context.Context, records []Record) error {
	groups := map[string][]Record{"": nil}
	for _, path := range m.directories.Known {
		groups[path] = nil
	}
	for _, r := range records {
		if !cacheID.MatchString(r.ID) || !cacheID.MatchString(r.Key) || r.Directory != "" && !validDirectory(r.Directory) {
			return errors.New("invalid online audio cache record")
		}
		groups[r.Directory] = append(groups[r.Directory], r)
	}
	for base, group := range groups {
		if err := m.restoreDirectory(ctx, base, group); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) restoreDirectory(ctx context.Context, base string, records []Record) error {
	dir, err := m.availableDirectory(base)
	var entries []fs.DirEntry
	var root *os.Root
	if err == nil {
		root, err = os.OpenRoot(dir)
	}
	if err == nil {
		defer root.Close()
		// 目录句柄固定到本次挂载。在此句柄内确认标记后再核对和回收文件。
		if base != "" {
			var marker []byte
			marker, err = root.ReadFile(ownerMarker)
			if err == nil && string(marker) != m.directoryMarker(base) {
				err = errors.New("online download directory ownership changed")
			}
		}
		if err == nil {
			entries, err = fs.ReadDir(root.FS(), ".")
		}
	}
	if err != nil {
		if base == "" {
			return err
		}
		slog.WarnContext(ctx, "Online download directory is unavailable; keeping its download records", "directory", base)
		for _, r := range records {
			m.records[r.ID], m.byKey[r.Key] = r, r.ID
		}
		return nil
	}
	valid := make(map[string]bool, len(records))
	for _, r := range records {
		name := r.ID + ".audio"
		info, err := root.Lstat(name)
		switch {
		case err != nil && !os.IsNotExist(err):
			slog.WarnContext(ctx, "Could not check downloaded audio; keeping its download record", "versionId", r.ID, "error", err)
		case err != nil || !info.Mode().IsRegular() || info.Size() != r.SizeBytes || r.SizeBytes <= 0:
			if err := m.repo.ForgetOnlineDownload(ctx, r.target()); err != nil {
				return err
			}
			continue
		}
		valid[name] = true
		m.records[r.ID], m.byKey[r.Key] = r, r.ID
	}
	for _, entry := range entries {
		name := entry.Name()
		id, audio := strings.CutSuffix(name, ".audio")
		if !entry.IsDir() && !valid[name] && (audio && cacheID.MatchString(id) || partialFile.MatchString(name)) {
			if err := root.Remove(name); err != nil {
				slog.WarnContext(ctx, "Could not clean up unused online audio file", "name", name, "error", err)
			}
		}
	}
	return nil
}
