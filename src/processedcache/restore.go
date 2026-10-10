package processedcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (m *Manager) restore(ctx context.Context) error {
	records, err := m.repo.ListProcessedCache(ctx)
	if err != nil {
		return err
	}
	if len(records) > MaxEntries {
		return errors.New("processed cache index exceeds its bound")
	}
	indexed := map[string]Record{}
	for _, r := range records {
		if r.Validate() == nil {
			indexed[r.ID+".mp3"] = r
		}
	}
	files, err := os.ReadDir(m.config.Root)
	if err != nil {
		return err
	}
	found := map[string]bool{}
	for _, file := range files {
		if err = ctx.Err(); err != nil {
			return err
		}
		name := file.Name()
		id, suffix, ok := strings.Cut(name, ".")
		if !ok || !validHex(id, 48) || (suffix != "part" && suffix != "mp3") {
			continue
		}
		path := filepath.Join(m.config.Root, name)
		info, statErr := os.Lstat(path)
		if statErr != nil {
			return statErr
		}
		if !info.Mode().IsRegular() {
			// Never follow or recursively remove an unexpected path.
			if info.Mode()&os.ModeSymlink != 0 {
				_ = os.Remove(path)
			}
			continue
		}
		if err = m.budget.adopt(info.Size()); err != nil {
			return err
		}
		r, exists := indexed[name]
		valid := exists && r.Size == info.Size() && m.entries[r.Key] == nil && m.now().Sub(r.CreatedAt) < m.config.HardTTL && m.now().Sub(r.LastUsedAt) < m.config.IdleTTL && m.budget.Used() <= m.budget.Limit()
		if valid {
			valid = verifyFile(ctx, path, r.SHA256) == nil
		}
		if valid {
			m.entries[r.Key] = &entry{record: r, info: info, lastRelease: r.LastUsedAt}
			found[r.ID] = true
		} else {
			m.garbage[path] = garbage{id: id, path: path, size: info.Size()}
		}
	}
	for _, r := range records {
		if !found[r.ID] {
			if err = m.repo.ForgetProcessedCache(ctx, r.ID); err != nil {
				return err
			}
		}
	}
	m.Collect(ctx)
	return nil
}

func verifyFile(ctx context.Context, path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	buffer := make([]byte, 64<<10)
	for {
		if err = ctx.Err(); err != nil {
			return err
		}
		n, readErr := f.Read(buffer)
		if n > 0 {
			h.Write(buffer[:n])
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if hex.EncodeToString(h.Sum(nil)) != expected {
		return errors.New("processed cache checksum mismatch")
	}
	return nil
}
