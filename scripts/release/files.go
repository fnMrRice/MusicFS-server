package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func copyRegular(source, dest string, limit int64) error {
	info, err := os.Lstat(source)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return fmt.Errorf("unsupported release input: %s", source)
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
		return err
	}
	output, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	n, err := io.Copy(output, io.LimitReader(input, limit+1))
	closeErr := output.Close()
	if err != nil {
		return err
	}
	if n != info.Size() || n > limit {
		return errors.New("release input changed while copying")
	}
	return closeErr
}

func (b builder) configuration() error {
	data, err := os.ReadFile(filepath.Join(b.root, "configs", "server.example.yaml"))
	if err != nil {
		return err
	}
	text := string(data)
	text = strings.ReplaceAll(text, "../build/windows/", "../bin/")
	if b.target == "linux" {
		for _, name := range programs {
			text = strings.ReplaceAll(text, name+".exe", name)
		}
	}
	text = strings.ReplaceAll(text, "../tests/testdata/audio", "../music")
	text = strings.ReplaceAll(text, "合成测试音乐", "本地音乐")
	text = strings.ReplaceAll(text, "# Replace with your music directory; this sample uses project fixtures.", "# Replace with your own music directory before scanning.")
	// Missing runtime dependencies remain pending; explicit opt-outs are preserved.
	if b.web != "" {
		text = strings.Replace(text, "webRoot: ''", "webRoot: '../web'", 1)
	}
	return os.WriteFile(filepath.Join(b.out, "configs", "server.example.yaml"), []byte(text), 0600)
}

func (b builder) copyWeb() error {
	allowed := map[string]bool{".html": true, ".js": true, ".css": true, ".json": true, ".svg": true, ".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".ico": true, ".woff": true, ".woff2": true}
	var count int
	var total int64
	return filepath.WalkDir(b.web, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("Web artifact must not contain links")
		}
		if path == b.web {
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			return errors.New("Web artifact must not contain hidden files")
		}
		if entry.IsDir() {
			return nil
		}
		if !allowed[strings.ToLower(filepath.Ext(path))] {
			return errors.New("Web artifact contains an unexpected file type")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		count++
		total += info.Size()
		if count > 4096 || total > 128<<20 {
			return errors.New("Web artifact exceeds release limits")
		}
		rel, err := filepath.Rel(b.web, path)
		if err != nil {
			return err
		}
		return copyRegular(path, filepath.Join(b.out, "web", rel), 32<<20)
	})
}

func (b builder) checksums() error {
	var lines []string
	err := filepath.WalkDir(b.out, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() == ".incomplete" {
			return nil
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		hash := sha256.New()
		_, err = io.Copy(hash, input)
		input.Close()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(b.out, path)
		if err != nil {
			return err
		}
		lines = append(lines, hex.EncodeToString(hash.Sum(nil))+"  "+filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(lines)
	return os.WriteFile(filepath.Join(b.out, "SHA256SUMS"), []byte(strings.Join(lines, "\n")+"\n"), 0600)
}
