package logging

import (
	"archive/tar"
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

var bundleName = regexp.MustCompile(`^logs-(\d{4}-\d{2}-\d{2})\.tar\.gz$`)
var bundleTemporaryName = regexp.MustCompile(`^\.musicfs-log-(logs-\d{4}-\d{2}-\d{2}\.tar\.gz)\.tmp-[A-Za-z0-9]+$`)

type archiveSource struct {
	path string
	name string
	info os.FileInfo
}

type archiveRecord struct {
	size   int64
	digest [sha256.Size]byte
}

func bundleDay(name string) (string, bool) {
	parts := bundleName.FindStringSubmatch(name)
	if len(parts) != 2 {
		return "", false
	}
	_, err := time.Parse(time.DateOnly, parts[1])
	return parts[1], err == nil
}

func bundleMember(name, day string) bool {
	plugins := strings.HasPrefix(name, "plugins/")
	base := strings.TrimPrefix(name, "plugins/")
	if strings.ContainsAny(base, `/\`) || !strings.HasSuffix(base, ".log") {
		return false
	}
	stamp, ok := ownedArchive(base, plugins)
	return ok && stamp.Format(time.DateOnly) == day
}

// 同一天的宿主与插件切片共同组成一个包；重建成功前保留旧包和全部原文。
func compressBundle(target string, sources []archiveSource) error {
	existing, err := os.Lstat(target)
	if err != nil && !os.IsNotExist(err) || err == nil && !existing.Mode().IsRegular() {
		return errors.New("log bundle destination is unavailable")
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), temporaryPrefix+filepath.Base(target)+".tmp-*")
	if err != nil {
		return errors.New("cannot create log bundle")
	}
	defer os.Remove(temporary.Name())
	defer temporary.Close()
	compressed := gzip.NewWriter(temporary)
	writer := tar.NewWriter(compressed)
	defer compressed.Close()
	defer writer.Close()
	pending := make(map[string]archiveSource, len(sources))
	for _, source := range sources {
		pending[source.name] = source
	}
	day, _ := bundleDay(filepath.Base(target))
	if existing != nil {
		err := readBundle(target, day, func(header *tar.Header, input io.Reader) error {
			if err := writer.WriteHeader(header); err != nil {
				return err
			}
			record, err := copyArchiveRecord(writer, input)
			if err != nil {
				return err
			}
			if source, ok := pending[header.Name]; ok {
				original, err := copyArchiveSource(io.Discard, source)
				if err != nil || original != record {
					return errors.New("log bundle member conflicts with its source")
				}
				delete(pending, header.Name)
			}
			return nil
		})
		if err != nil {
			return errors.New("cannot merge existing log bundle")
		}
	}
	for _, source := range sources {
		if _, ok := pending[source.name]; !ok {
			continue
		}
		header, err := tar.FileInfoHeader(source.info, "")
		if err != nil {
			return errors.New("cannot describe log bundle member")
		}
		header.Name = source.name
		if err := writer.WriteHeader(header); err != nil {
			return errors.New("cannot write log bundle member")
		}
		if _, err := copyArchiveSource(writer, source); err != nil {
			return err
		}
	}
	err = errors.Join(writer.Close(), compressed.Close())
	if err == nil {
		err = temporary.Sync()
	}
	err = errors.Join(err, temporary.Close())
	if err != nil {
		return errors.New("cannot finish log bundle")
	}
	for _, source := range sources {
		if !sameArchive(source.path, source.info) {
			return errors.New("log archive changed during bundling")
		}
	}
	if existing != nil && !sameArchive(target, existing) || existing == nil && !absent(target) {
		return errors.New("log bundle changed before publication")
	}
	if err := os.Rename(temporary.Name(), target); err != nil {
		return errors.New("cannot publish log bundle")
	}
	var result error
	for _, source := range sources {
		result = errors.Join(result, removeArchive(source.path, source.info))
	}
	return result
}

func copyArchiveRecord(output io.Writer, input io.Reader) (archiveRecord, error) {
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(output, hash), input)
	record := archiveRecord{size: size}
	copy(record.digest[:], hash.Sum(nil))
	return record, err
}

func copyArchiveSource(output io.Writer, source archiveSource) (archiveRecord, error) {
	file, err := os.Open(source.path)
	if err != nil {
		return archiveRecord{}, errors.New("cannot open log archive")
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil || !os.SameFile(info, source.info) {
		return archiveRecord{}, errors.New("log archive changed before bundling")
	}
	record, err := copyArchiveRecord(output, io.LimitReader(file, source.info.Size()+1))
	if err != nil || record.size != source.info.Size() || !sameArchive(source.path, source.info) {
		return record, errors.New("cannot read complete log archive")
	}
	return record, nil
}

// tar 结束后仍读取 gzip 尾部，避免把缺少校验和的截断包当成完整归档。
func readBundle(path, day string, visit func(*tar.Header, io.Reader) error) error {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("log bundle is unavailable")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	if opened, err := file.Stat(); err != nil || !os.SameFile(info, opened) {
		return errors.New("log bundle changed before reading")
	}
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	seen := map[string]bool{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg || !bundleMember(header.Name, day) || seen[header.Name] {
			return errors.New("log bundle contains an invalid member")
		}
		seen[header.Name] = true
		if err := visit(header, reader); err != nil {
			return err
		}
	}
	if _, err := io.Copy(io.Discard, compressed); err != nil {
		return err
	}
	if !sameArchive(path, info) {
		return errors.New("log bundle changed during reading")
	}
	return nil
}
