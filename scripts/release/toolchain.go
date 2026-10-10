package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func (b builder) toolchainLicenses() ([]string, error) {
	output, err := b.command("go", "env", "GOROOT")
	if err != nil {
		return nil, err
	}
	root := strings.TrimSpace(string(output))
	if !filepath.IsAbs(root) {
		return nil, errors.New("Go toolchain root must be absolute")
	}
	var result []string
	for _, name := range []string{"LICENSE", "PATENTS"} {
		path := filepath.Join(root, name)
		if _, err = os.Lstat(path); name == "PATENTS" && os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		rel := filepath.Join("licenses", "go", name)
		if err = copyRegular(path, filepath.Join(b.out, rel), 4<<20); err != nil {
			return nil, err
		}
		result = append(result, filepath.ToSlash(rel))
	}
	return result, nil
}
