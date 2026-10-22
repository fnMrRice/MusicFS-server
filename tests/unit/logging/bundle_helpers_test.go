package logging_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func archiveFixture(t *testing.T, directory, id string, stamp time.Time, data string) string {
	t.Helper()
	name := id + "-" + stamp.UTC().Format("2006-01-02T15-04-05.000") + ".log"
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func bundlePath(directory string, stamp time.Time) string {
	return filepath.Join(directory, "logs-"+stamp.UTC().Format(time.DateOnly)+".tar.gz")
}

func bundlePaths(t *testing.T, directory string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(directory, "logs-*.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func bundleFixture(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(gz)
	for name, data := range entries {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(writer, data); err != nil {
			t.Fatal(err)
		}
	}
	if err := errors.Join(writer.Close(), gz.Close()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buffer.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func visitBundle(path string, visit func(string, io.Reader) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	seen := make(map[string]bool)
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			_, err = io.Copy(io.Discard, gz)
			return err
		}
		if err != nil {
			return err
		}
		if header.Typeflag != tar.TypeReg || seen[header.Name] {
			return errors.New("archive contains a non-regular or duplicate member")
		}
		seen[header.Name] = true
		if err := visit(header.Name, reader); err != nil {
			return err
		}
	}
}

func loadBundle(path string) (map[string]string, error) {
	entries := make(map[string]string)
	err := visitBundle(path, func(name string, reader io.Reader) error {
		data, err := io.ReadAll(reader)
		entries[name] = string(data)
		return err
	})
	return entries, err
}

func readBundle(t *testing.T, path string) map[string]string {
	t.Helper()
	entries, err := loadBundle(path)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func waitBundle(t *testing.T, path string, count int) map[string]string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := loadBundle(path)
		if err == nil && len(entries) == count {
			return entries
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("archive did not contain %d complete members: %s", count, path)
	return nil
}

func bundleStats(t *testing.T, path string) (int, int64) {
	t.Helper()
	var count int
	var total int64
	err := visitBundle(path, func(_ string, reader io.Reader) error {
		size, err := io.Copy(io.Discard, reader)
		count++
		total += size
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return count, total
}

func hasBundleContent(entries map[string]string, data string) bool {
	for _, content := range entries {
		if content == data {
			return true
		}
	}
	return false
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("file should have been removed: %s (%v)", path, err)
	}
}
