package pluginversions

import (
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
		if !ok || working.owners(name, nil) {
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

// Resolve 沿第一父提交统计每次进入当前分支的插件更新，合并提交计一次。
// 工作树相对于 HEAD 的合计改动同样只计一次，构建不会写入版本文件。
func Resolve(root string) (map[string]string, error) {
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
	base, err := r.git("log", "-1", "--first-parent", "--diff-merges=first-parent", "--diff-filter=A", "--no-patch", "--format=%H", "--", manifestPath)
	if err != nil {
		return nil, err
	}
	base = strings.TrimSpace(base)
	shallow, err := r.git("rev-parse", "--is-shallow-repository")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(shallow) == "true" {
		name, err := r.git("rev-parse", "--path-format=absolute", "--git-path", "shallow")
		if err != nil {
			return nil, err
		}
		boundary, err := os.ReadFile(strings.TrimSpace(name))
		if err != nil {
			return nil, err
		}
		if base == "" || slices.Contains(strings.Fields(string(boundary)), base) {
			return nil, fmt.Errorf("plugin version history is incomplete; fetch history through the release declaration commit")
		}
	}
	versions := map[string]string{}
	// 首次引入版本声明时按初始值交付，不将此前的实现历史重复计入版本。
	if base == "" {
		return versions, advance(versions, catalog{}, working, working, nil)
	}
	previous, err := r.catalogAt(base)
	if err != nil {
		return nil, err
	}
	if err = advance(versions, catalog{}, previous, working, nil); err != nil {
		return nil, err
	}
	history, err := r.git("log", "--reverse", "--first-parent", "--diff-merges=first-parent", "--no-renames", "--format=%x00rev:%H%x00", "--name-only", "-z", base+"..HEAD", "--", "plugins", manifestPath)
	if err != nil {
		return nil, err
	}
	var revision string
	var files []string
	process := func() error {
		next := previous
		if slices.Contains(files, manifestPath) {
			var err error
			next, err = r.catalogAt(revision)
			if err != nil {
				return err
			}
		}
		err := advance(versions, previous, next, working, files)
		previous = next
		return err
	}
	for token := range strings.SplitSeq(history, "\x00") {
		token = strings.TrimPrefix(token, "\n")
		if token == "" {
			continue
		}
		if rev, header := strings.CutPrefix(token, "rev:"); header {
			if err := process(); err != nil {
				return nil, err
			}
			revision, files = rev, nil
		} else {
			files = append(files, token)
		}
	}
	if err := process(); err != nil {
		return nil, err
	}
	tracked, err := r.git("diff", "HEAD", "--name-only", "--no-renames", "-z", "--", "plugins")
	if err != nil {
		return nil, err
	}
	untracked, err := r.git("ls-files", "--others", "--exclude-standard", "-z", "--", "plugins")
	if err != nil {
		return nil, err
	}
	return versions, advance(versions, previous, working, working, strings.Split(tracked+untracked, "\x00"))
}

// LinkFlags 将同一份版本结果用于宿主、统一 worker 和独立插件程序。
func LinkFlags(versions map[string]string) string {
	values := make([]string, 0, len(versions))
	for _, id := range slices.Sorted(maps.Keys(versions)) {
		values = append(values, id+"="+versions[id])
	}
	return "-X=github.com/fnMrRice/MusicFS-server/sdk/bundledplugin.buildVersions=" + strings.Join(values, ",")
}
