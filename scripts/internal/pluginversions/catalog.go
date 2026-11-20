// Package pluginversions 按官方插件的实现文件计算构建版本。
package pluginversions

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const manifestPath = "sdk/bundledplugin/releases.json"

type plugin struct {
	Version string
	Sources []string
}

type catalog struct {
	Groups  map[string][]string
	Plugins map[string]plugin
}

var semver = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func parseCatalog(data []byte) (catalog, error) {
	var c catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return c, err
	}
	for id, p := range c.Plugins {
		if !semver.MatchString(p.Version) || len(p.Sources) == 0 {
			return c, fmt.Errorf("invalid release declaration for %s", id)
		}
		rules, err := c.expand(p.Sources, nil)
		if err != nil {
			return c, err
		}
		p.Sources = rules
		c.Plugins[id] = p
	}
	return c, nil
}

func (c catalog) expand(rules, parents []string) ([]string, error) {
	var result []string
	for _, rule := range rules {
		name, group := strings.CutPrefix(rule, "@")
		if !group {
			if _, err := path.Match(rule, ""); err != nil {
				return nil, err
			}
			result = append(result, rule)
			continue
		}
		items, ok := c.Groups[name]
		if !ok || slices.Contains(parents, name) {
			return nil, fmt.Errorf("invalid release source group %q", name)
		}
		expanded, err := c.expand(items, append(parents, name))
		if err != nil {
			return nil, err
		}
		result = append(result, expanded...)
	}
	return result, nil
}

func implementation(file string) (string, bool) {
	name, ok := strings.CutPrefix(file, "plugins/")
	if !ok || strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, ".md") {
		return "", false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "tests" || part == "testkit" || part == "testdata" {
			return "", false
		}
	}
	return name, true
}

func (p plugin) owns(name string) bool {
	for _, rule := range p.Sources {
		if strings.HasSuffix(rule, "/") && strings.HasPrefix(name, rule) {
			return true
		}
		if matched, _ := path.Match(rule, name); matched {
			return true
		}
	}
	return false
}

func (c catalog) owners(name string, affected map[string]bool) bool {
	found := false
	for id, p := range c.Plugins {
		if p.owns(name) {
			if affected == nil {
				return true
			}
			affected[id], found = true, true
		}
	}
	return found
}

// 新插件或显式重设的基准版本从声明值开始，该提交的多个文件只递增一次。
func advance(current map[string]string, before, after, working catalog, files []string) error {
	affected := map[string]bool{}
	for _, file := range files {
		name, ok := implementation(file)
		if !ok {
			continue
		}
		oldOwner := before.owners(name, affected)
		newOwner := after.owners(name, affected)
		// 允许后续提交补登记遗漏的归属，历史更新仍按原提交计数。
		if !oldOwner && !newOwner {
			working.owners(name, affected)
		}
	}
	for id := range current {
		if _, ok := after.Plugins[id]; !ok {
			delete(current, id)
		}
	}
	for id, p := range after.Plugins {
		if before.Plugins[id].Version != p.Version {
			current[id] = p.Version
			continue
		}
		if !affected[id] {
			continue
		}
		parts := strings.Split(current[id], ".")
		minor, err := strconv.ParseUint(parts[1], 10, 63)
		if err != nil {
			return fmt.Errorf("invalid minor version for %s: %w", id, err)
		}
		current[id] = fmt.Sprintf("%s.%d.0", parts[0], minor+1)
	}
	return nil
}
