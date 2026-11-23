package pluginversions

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

type repository string

func (r repository) git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = string(r)
	b, err := cmd.Output()
	if err != nil {
		if failure, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %w: %s", args[0], err, failure.Stderr)
		}
		return "", err
	}
	return string(b), nil
}

func (r repository) catalogAt(revision string) (catalog, error) {
	data, err := r.git("show", revision+":"+manifestPath)
	if err != nil {
		return catalog{}, err
	}
	return parseCatalog([]byte(data))
}

func (r repository) validateSources(working catalog) error {
	list, err := r.git("ls-files", "--cached", "--others", "--exclude-standard", "-z", "--", "plugins")
	if err != nil {
		return err
	}
	for file := range strings.SplitSeq(list, "\x00") {
		name, ok := implementation(file)
		if !ok || working.owners(name, nil, unchanged) {
			continue
		}
		// ls-files 还会列出已在工作树删除但尚未暂存的文件。
		if _, err := os.Lstat(filepath.Join(string(r), filepath.FromSlash(file))); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		return fmt.Errorf("declare the plugin that owns %s in %s", file, manifestPath)
	}
	return nil
}

//go:embed release-baseline.json
var firstRelease []byte

// Resolve 从已发布的插件版本开始，计算当前提交所属批次的候选版本。
func Resolve(root string) (map[string]string, error) {
	var base struct {
		Revision string
		Versions map[string]string
	}
	if err := json.Unmarshal(firstRelease, &base); err != nil {
		return nil, err
	}
	return resolve(root, base.Revision, base.Versions)
}

func resolve(root, base string, published map[string]string) (map[string]string, error) {
	r := repository(root)
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(manifestPath)))
	if err != nil {
		return nil, err
	}
	working, err := parseCatalog(raw)
	if err != nil {
		return nil, err
	}
	if err := r.validateSources(working); err != nil {
		return nil, err
	}
	head, err := r.git("rev-parse", "--verify", "HEAD")
	if err != nil {
		return nil, err
	}
	ends, err := r.releaseEnds(base, strings.TrimSpace(head))
	if err != nil {
		return nil, err
	}
	first, err := r.catalogAt(base)
	if err != nil {
		return nil, err
	}
	if len(first.Plugins) != len(published) {
		return nil, fmt.Errorf("plugin release baseline does not match catalog at %s", base)
	}
	for id := range first.Plugins {
		version := published[id]
		if !semver.MatchString(version) || !strings.HasPrefix(version, "0.") {
			return nil, fmt.Errorf("missing or invalid published version for %s", id)
		}
	}
	boundaries, err := r.shallowBoundaries()
	if err != nil {
		return nil, err
	}
	versions := maps.Clone(published)
	for _, end := range ends {
		versions, err = r.batch(base, end, versions, boundaries)
		if err != nil {
			return nil, err
		}
		base = end
	}
	return versions, nil
}

func (r repository) shallowBoundaries() ([]string, error) {
	shallow, err := r.git("rev-parse", "--is-shallow-repository")
	if err != nil || strings.TrimSpace(shallow) != "true" {
		return nil, err
	}
	name, err := r.git("rev-parse", "--path-format=absolute", "--git-path", "shallow")
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(strings.TrimSpace(name))
	return strings.Fields(string(data)), err
}

// LinkFlags 将同一份版本结果用于宿主、统一 worker 和独立插件程序。
func LinkFlags(versions map[string]string) string {
	values := make([]string, 0, len(versions))
	for _, id := range slices.Sorted(maps.Keys(versions)) {
		values = append(values, id+"="+versions[id])
	}
	return "-X=github.com/fnMrRice/MusicFS-server/sdk/bundledplugin.buildVersions=" + strings.Join(values, ",")
}
