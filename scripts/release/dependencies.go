package main

import (
	"bytes"
	"encoding/json"
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
	Path       string   `json:"path"`
	Version    string   `json:"version"`
	Sum        string   `json:"sum"`
	Compiled   bool     `json:"compiled"`
	Licenses   []string `json:"licenses"`
	Modules    []string `json:"modules"`
	FirstParty bool     `json:"firstParty,omitempty"`
	Source     string   `json:"source,omitempty"`
}

func (b builder) dependencies() ([]dependency, error) {
	compiled, err := b.compiledModules()
	if err != nil {
		return nil, err
	}
	deps := map[string]dependency{}
	licenseModules := map[string]module{}
	for _, source := range sourceModules {
		data, err := b.moduleCommand(source.Directory, "go", "list", "-mod=readonly", "-m", "-json", "all")
		if err != nil {
			return nil, err
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
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
				if mod.Path != source.Path {
					return nil, fmt.Errorf("unexpected source module: %s", mod.Path)
				}
				continue
			}
			localSource, err := b.reviewedReplacement(mod)
			if err != nil {
				return nil, err
			}
			key := moduleKey(mod)
			dep, found := deps[key]
			if found && (dep.FirstParty != (localSource != "") || dep.Source != localSource ||
				(dep.Sum != "" && mod.Sum != "" && dep.Sum != mod.Sum)) {
				return nil, fmt.Errorf("inconsistent module source across release programs: %s", key)
			}
			if !found {
				dep = dependency{Path: mod.Path, Version: mod.Version, Sum: mod.Sum, Compiled: compiled[key], Licenses: []string{}, FirstParty: localSource != ""}
				if localSource != "" {
					dep.Source = localSource
					dep.Compiled = dep.Compiled || compiled[mod.Path+"@"]
				}
			}
			if dep.Sum == "" {
				dep.Sum = mod.Sum
			}
			dep.Modules = append(dep.Modules, source.Path)
			deps[key] = dep
			if candidate, found := licenseModules[key]; !found || candidate.Dir == "" {
				licenseModules[key] = mod
			}
		}
	}
	result := make([]dependency, 0, len(deps))
	for key, dep := range deps {
		if !dep.FirstParty {
			dep.Licenses, err = b.dependencyLicenses(licenseModules[key], dep.Compiled)
			if err != nil {
				return nil, err
			}
		}
		result = append(result, dep)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Path+"@"+result[i].Version < result[j].Path+"@"+result[j].Version
	})
	return result, nil
}

func moduleKey(mod module) string { return mod.Path + "@" + mod.Version }

func (b builder) compiledModules() (map[string]bool, error) {
	result := map[string]bool{}
	for _, source := range sourceModules {
		args := []string{"list", "-mod=readonly", "-buildvcs=false", "-deps", "-json"}
		optionCount := len(args)
		for _, program := range b.programs() {
			if program.moduleDir == source.Directory {
				args = append(args, program.packagePath)
			}
		}
		if len(args) == optionCount {
			continue
		}
		data, err := b.moduleCommand(source.Directory, "go", args...)
		if err != nil {
			return nil, err
		}
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
				result[moduleKey(*pkg.Module)] = true
			}
		}
	}
	return result, nil
}

// Only the SDK and online modules in this checkout are approved replacements.
// Arbitrary directory replacements could bypass dependency/license accounting.
func (b builder) reviewedReplacement(mod module) (string, error) {
	if mod.Replace == nil {
		return "", nil
	}
	for _, source := range sourceModules[1:] {
		if mod.Path != source.Path {
			continue
		}
		want, wantErr := filepath.EvalSymlinks(filepath.Join(b.root, source.Directory))
		got, gotErr := filepath.EvalSymlinks(mod.Replace.Dir)
		if mod.Replace.Version == "" && wantErr == nil && gotErr == nil && sameDirectory(want, got) {
			return source.Directory, nil
		}
	}
	return "", fmt.Errorf("release module replacement is not an approved repository module: %s", mod.Path)
}

func sameDirectory(left, right string) bool {
	a, err := os.Stat(left)
	if err != nil {
		return false
	}
	b, err := os.Stat(right)
	return err == nil && os.SameFile(a, b)
}

func (b builder) dependencyLicenses(mod module, compiled bool) ([]string, error) {
	result := []string{}
	if mod.Dir != "" {
		entries, err := os.ReadDir(mod.Dir)
		if err != nil {
			return nil, err
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
			result = append(result, filepath.ToSlash(rel))
		}
	}
	if compiled && len(result) == 0 {
		return nil, fmt.Errorf("compiled dependency has no collected license: %s", mod.Path)
	}
	return result, nil
}
