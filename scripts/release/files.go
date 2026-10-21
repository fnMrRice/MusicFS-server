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

	"go.yaml.in/yaml/v3"
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
		for _, program := range releasePrograms {
			text = strings.ReplaceAll(text, program.name+".exe", program.name)
		}
	}
	// Missing runtime dependencies remain pending; explicit opt-outs are preserved.
	if b.web != "" {
		text = strings.Replace(text, "webRoot: ''", "webRoot: '../web'", 1)
	}
	data, err = b.requiredStorageConfiguration(text)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(b.out, "configs", "server.example.yaml"), data, 0600)
}

func (b builder) requiredStorageConfiguration(text string) ([]byte, error) {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(text), &document); err != nil || len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("release configuration is not a YAML mapping")
	}
	var storage yaml.Node
	if err := storage.Encode(map[string]any{"backend": "sqlite"}); err != nil {
		return nil, err
	}
	mapping := document.Content[0]
	found := false
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == "storage" {
			mapping.Content[i+1] = &storage
			found = true
			break
		}
	}
	if !found {
		mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "storage"}, &storage)
	}
	return yaml.Marshal(&document)
}

func (b builder) containerConfiguration() error {
	if b.target != "linux" {
		return nil
	}
	path := filepath.Join(b.out, "deploy", "server.container.example.yaml")
	source, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	data, err := b.requiredStorageConfiguration(string(source))
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
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
