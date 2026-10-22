package logging

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
)

// 临时包只有在每个成员都有另一份完整副本时才能删除；损坏或唯一副本留待恢复。
func recoverBundleTemporary(path, directory string) error {
	parts := bundleTemporaryName.FindStringSubmatch(filepath.Base(path))
	if len(parts) != 2 {
		return nil
	}
	day, ok := bundleDay(parts[1])
	if !ok {
		return nil
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	records, err := bundleRecords(path, day)
	if err != nil {
		return nil
	}
	target := filepath.Join(directory, parts[1])
	existing, _ := os.Lstat(target)
	published, err := bundleRecords(target, day)
	if err != nil {
		published = nil
	}
	var sources []archiveSource
	usedPublished := false
	for name, record := range records {
		if stored, ok := published[name]; ok && stored == record {
			usedPublished = true
			continue
		}
		sourcePath := filepath.Join(directory, filepath.FromSlash(name))
		sourceInfo, err := os.Lstat(sourcePath)
		if err != nil || !sourceInfo.Mode().IsRegular() {
			return nil
		}
		source := archiveSource{path: sourcePath, name: name, info: sourceInfo}
		original, err := copyArchiveSource(io.Discard, source)
		if err != nil || original != record {
			return nil
		}
		sources = append(sources, source)
	}
	if usedPublished && (existing == nil || !sameArchive(target, existing)) {
		return nil
	}
	for _, source := range sources {
		if !sameArchive(source.path, source.info) {
			return nil
		}
	}
	return removeArchive(path, info)
}

func bundleRecords(path, day string) (map[string]archiveRecord, error) {
	records := map[string]archiveRecord{}
	err := readBundle(path, day, func(header *tar.Header, input io.Reader) error {
		record, err := copyArchiveRecord(io.Discard, input)
		records[header.Name] = record
		return err
	})
	return records, err
}
