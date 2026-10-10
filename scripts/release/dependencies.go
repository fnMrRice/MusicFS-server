package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type module struct {
	Path, Version, Sum, Dir string
	Main                    bool
	Replace                 *module
}
type dependency struct {
	Path     string   `json:"path"`
	Version  string   `json:"version"`
	Sum      string   `json:"sum"`
	Compiled bool     `json:"compiled"`
	Licenses []string `json:"licenses"`
}

func (b builder) dependencies() ([]dependency, error) {
	args := []string{"list", "-mod=readonly", "-deps", "-json"}
	for _, name := range programs {
		args = append(args, "./src/app/"+name)
	}
	data, err := b.command("go", args...)
	if err != nil {
		return nil, err
	}
	compiled := map[string]bool{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var pkg struct{ Module *module }
		err = decoder.Decode(&pkg)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if pkg.Module != nil {
			compiled[pkg.Module.Path] = true
		}
	}
	data, err = b.command("go", "list", "-mod=readonly", "-m", "-json", "all")
	if err != nil {
		return nil, err
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	var result []dependency
	for {
		var mod module
		err = decoder.Decode(&mod)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if mod.Main {
			continue
		}
		if mod.Replace != nil {
			return nil, errors.New("release module replacements require an explicitly reviewed packaging policy")
		}
		dep := dependency{Path: mod.Path, Version: mod.Version, Sum: mod.Sum, Compiled: compiled[mod.Path], Licenses: []string{}}
		if mod.Dir != "" {
			entries, readErr := os.ReadDir(mod.Dir)
			if readErr != nil {
				return nil, readErr
			}
			for _, entry := range entries {
				upper := strings.ToUpper(entry.Name())
				if entry.IsDir() || !(strings.HasPrefix(upper, "LICENSE") || strings.HasPrefix(upper, "LICENCE") || strings.HasPrefix(upper, "COPYING") || strings.HasPrefix(upper, "NOTICE")) {
					continue
				}
				rel := filepath.Join("licenses", mod.Path+"@"+mod.Version, entry.Name())
				if err = copyRegular(filepath.Join(mod.Dir, entry.Name()), filepath.Join(b.out, rel), 4<<20); err != nil {
					return nil, err
				}
				dep.Licenses = append(dep.Licenses, filepath.ToSlash(rel))
			}
		}
		if dep.Compiled && len(dep.Licenses) == 0 {
			return nil, fmt.Errorf("compiled dependency has no collected license: %s", mod.Path)
		}
		result = append(result, dep)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}
