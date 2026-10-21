package logging

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const archiveTime = "2006-01-02T15-04-05.000"
const temporaryPrefix = ".musicfs-log-"

var archiveName = regexp.MustCompile(`^(.+)-(\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}\.\d{3})\.log(?:\.gz)?$`)
var temporaryName = regexp.MustCompile(`^\.musicfs-log-(.+\.log)\.gz\.tmp-[A-Za-z0-9]+$`)
var errArchiveTooLarge = errors.New("compressed log archive exceeds its expected size")

// cleanup 由维护线程串行调用；启动及关闭时调用方确保维护线程未运行。
func (m *Manager) cleanup(now time.Time) error {
	m.mu.RLock()
	active := make(map[string]bool, len(m.sinks))
	for id := range m.sinks {
		active[id] = true
	}
	m.mu.RUnlock()
	var result error
	for _, plugins := range []bool{false, true} {
		directory := m.directory
		if plugins {
			directory = filepath.Join(directory, "plugins")
		}
		entries, err := os.ReadDir(directory)
		if err != nil {
			result = errors.Join(result, errors.New("cannot inspect log archives"))
			continue
		}
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			path := filepath.Join(directory, entry.Name())
			if plugins && strings.HasSuffix(entry.Name(), ".log") && active[strings.TrimSuffix(entry.Name(), ".log")] {
				continue
			}
			if parts := temporaryName.FindStringSubmatch(entry.Name()); len(parts) == 2 {
				if _, ok := ownedArchive(parts[1], plugins); ok {
					result = errors.Join(result, recoverTemporary(path, filepath.Join(directory, parts[1])))
				}
				continue
			}
			if stamp, ok := ownedArchive(entry.Name(), plugins); ok {
				if stamp.Before(now.UTC().Add(-RetentionDays * 24 * time.Hour)) {
					result = errors.Join(result, removeArchive(path, info))
				} else if !strings.HasSuffix(path, ".gz") {
					result = errors.Join(result, compressArchive(path))
				}
				continue
			}
			id := strings.TrimSuffix(entry.Name(), ".log")
			if !plugins || id == entry.Name() || !ValidPluginID(id) || active[id] {
				continue
			}
			archived, err := m.archiveInactive(path, id, now)
			if err == nil && archived != "" {
				err = compressArchive(archived)
			}
			result = errors.Join(result, err)
		}
	}
	return result
}

func ownedArchive(name string, plugins bool) (time.Time, bool) {
	parts := archiveName.FindStringSubmatch(name)
	if len(parts) != 3 || plugins && !ValidPluginID(parts[1]) || !plugins && parts[1] != "server" {
		return time.Time{}, false
	}
	stamp, err := time.Parse(archiveTime, parts[2])
	return stamp, err == nil
}

// 检查与重命名共用 sink 锁，防止新插件打开同名活动文件后被误归档。
func (m *Manager) archiveInactive(path, id string, now time.Time) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sinks[id] != nil {
		return "", nil
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) || err == nil && !info.Mode().IsRegular() {
		return "", nil
	}
	if err != nil {
		return "", errors.New("cannot inspect inactive plugin log")
	}
	if info.ModTime().UTC().Format(time.DateOnly) >= now.UTC().Format(time.DateOnly) {
		return "", nil
	}
	if info.ModTime().Before(now.Add(-RetentionDays * 24 * time.Hour)) {
		return "", removeArchive(path, info)
	}
	for offset := 0; offset < 1000; offset++ {
		stamp := info.ModTime().UTC().Add(time.Duration(offset) * time.Millisecond).Format(archiveTime)
		target := filepath.Join(filepath.Dir(path), id+"-"+stamp+".log")
		if !absent(target) || !absent(target+".gz") {
			continue
		}
		if err := os.Rename(path, target); err != nil {
			return "", errors.New("cannot archive inactive plugin log")
		}
		return target, nil
	}
	return "", errors.New("inactive plugin log archive names are exhausted")
}

// 临时文件不是唯一副本时才删除重试；缺少原文及有效归档时保留供人工恢复。
func recoverTemporary(path, source string) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	if original, err := os.Lstat(source); err == nil && original.Mode().IsRegular() {
		return removeArchive(path, info)
	}
	if _, _, err := gzipDigest(source+".gz", 1<<30); err == nil {
		return removeArchive(path, info)
	}
	return nil
}

func compressArchive(path string) error {
	source, err := os.Lstat(path)
	if os.IsNotExist(err) || err == nil && !source.Mode().IsRegular() {
		return nil
	}
	if err != nil {
		return errors.New("cannot inspect uncompressed log archive")
	}
	input, err := os.Open(path)
	if err != nil {
		return errors.New("cannot open log archive")
	}
	defer input.Close()
	if opened, err := input.Stat(); err != nil || !os.SameFile(source, opened) {
		return errors.New("log archive changed before compression")
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), temporaryPrefix+filepath.Base(path)+".gz.tmp-*")
	if err != nil {
		return errors.New("cannot create compressed log archive")
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	hash := sha256.New()
	writer := gzip.NewWriter(temporary)
	writer.ModTime = source.ModTime().UTC()
	written, copyErr := io.Copy(writer, io.TeeReader(input, hash))
	err = errors.Join(copyErr, writer.Close())
	if err == nil {
		err = temporary.Sync()
	}
	err = errors.Join(err, temporary.Close(), input.Close())
	if err != nil || written != source.Size() || !sameArchive(path, source) {
		return errors.New("cannot finish compressed log archive")
	}
	target := path + ".gz"
	existing, statErr := os.Lstat(target)
	if statErr != nil && !os.IsNotExist(statErr) || statErr == nil && !existing.Mode().IsRegular() {
		return errors.New("compressed log archive destination is unavailable")
	}
	if statErr == nil {
		digest, size, checkErr := gzipDigest(target, source.Size())
		if checkErr == nil {
			if size != written || !bytes.Equal(digest[:], hash.Sum(nil)) {
				return errors.New("compressed log archive conflicts with its source")
			}
			if !sameArchive(target, existing) {
				return errors.New("compressed log archive changed during verification")
			}
			return removeArchive(path, source)
		}
		if errors.Is(checkErr, errArchiveTooLarge) {
			return errors.New("compressed log archive conflicts with its source")
		}
		// 完整原文仍在时可重建损坏 gzip；有效且不同的归档永远不会被覆盖。
		if !sameArchive(target, existing) {
			return errors.New("compressed log archive changed during recovery")
		}
	} else if !absent(target) {
		return errors.New("compressed log archive appeared during compression")
	}
	if err := os.Rename(temporary.Name(), target); err != nil {
		return errors.New("cannot publish compressed log archive")
	}
	return removeArchive(path, source)
}

func gzipDigest(path string, limit int64) ([32]byte, int64, error) {
	var digest [32]byte
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return digest, 0, errors.New("compressed log archive is unavailable")
	}
	file, err := os.Open(path)
	if err != nil {
		return digest, 0, err
	}
	defer file.Close()
	if opened, err := file.Stat(); err != nil || !os.SameFile(info, opened) {
		return digest, 0, errors.New("compressed log archive changed before verification")
	}
	reader, err := gzip.NewReader(file)
	if err != nil {
		return digest, 0, err
	}
	defer reader.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(reader, limit+1))
	if size > limit {
		return digest, size, errArchiveTooLarge
	}
	if err != nil || !sameArchive(path, info) {
		return digest, size, errors.New("compressed log archive is invalid or changed")
	}
	copy(digest[:], hash.Sum(nil))
	return digest, size, nil
}

func sameArchive(path string, expected os.FileInfo) bool {
	current, err := os.Lstat(path)
	return err == nil && current.Mode().IsRegular() && os.SameFile(current, expected) && current.Size() == expected.Size() && current.ModTime().Equal(expected.ModTime())
}

func absent(path string) bool {
	_, err := os.Lstat(path)
	return os.IsNotExist(err)
}

func removeArchive(path string, expected os.FileInfo) error {
	if !sameArchive(path, expected) {
		if absent(path) {
			return nil
		}
		return errors.New("log archive changed before removal")
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return errors.New("cannot remove log archive")
	}
	return nil
}
