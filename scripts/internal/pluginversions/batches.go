package pluginversions

import (
	"fmt"
	"slices"
	"strings"
)

// 发布提交可能是空提交，也可能只更新产品版本；识别批次时须读取完整第一父链。
func (r repository) releaseEnds(base, head string) ([]string, error) {
	raw, err := r.git("log", "--first-parent", "--reverse", "--format=%H %P%n%B", "-z", base+".."+head)
	if err != nil {
		return nil, fmt.Errorf("incomplete plugin release history from %s: %w", base, err)
	}
	var ends []string
	tip := base
	for record := range strings.SplitSeq(raw, "\x00") {
		if record == "" {
			continue
		}
		header, message, _ := strings.Cut(record, "\n")
		hashes := strings.Fields(header)
		if len(hashes) < 2 || hashes[1] != tip {
			return nil, fmt.Errorf("incomplete first-parent plugin release history from %s", base)
		}
		tip = hashes[0]
		title, _, _ := strings.Cut(message, "\n")
		if releaseTitle.MatchString(title) {
			if err := r.verifyReleaseVersion(tip, strings.TrimPrefix(title, "chore(release): publish v")); err != nil {
				return nil, err
			}
			ends = append(ends, tip)
		}
	}
	if tip != head {
		return nil, fmt.Errorf("release baseline %s is outside the first-parent history of %s", base, head)
	}
	if head != base && (len(ends) == 0 || ends[len(ends)-1] != head) {
		ends = append(ends, head)
	}
	return ends, nil
}

func (r repository) batch(base, end string, published map[string]string, shallow []string) (map[string]string, error) {
	current, err := r.catalogAt(base)
	if err != nil {
		return nil, err
	}
	final, err := r.catalogAt(end)
	if err != nil {
		return nil, err
	}
	history, err := r.commitHistory([]string{base + ".." + end})
	if err != nil {
		return nil, err
	}
	affected := map[string]increment{}
	for entry := range history {
		next := current
		if slices.Contains(entry.files, manifestPath) {
			next, err = r.catalogAt(entry.revision)
			if err != nil {
				return nil, err
			}
		}
		if slices.ContainsFunc(entry.files, func(file string) bool {
			_, ok := implementation(file)
			return ok
		}) {
			change, err := r.commitChanges(entry, shallow)
			if err != nil {
				return nil, err
			}
			for _, file := range entry.files {
				bump := change.forFile(file)
				name, ok := implementation(file)
				if !ok || bump == unchanged {
					continue
				}
				before := current.owners(name, affected, bump)
				after := next.owners(name, affected, bump)
				if !before && !after {
					final.owners(name, affected, bump)
				}
			}
		}
		current = next
	}
	versions := make(map[string]string, len(final.Plugins))
	for id, plugin := range final.Plugins {
		version, exists := published[id]
		switch {
		case !exists:
			version = plugin.Version
		case affected[id] != unchanged:
			version, err = affected[id].apply(version)
			if err != nil {
				return nil, fmt.Errorf("advance plugin version for %s: %w", id, err)
			}
		}
		versions[id] = version
	}
	return versions, nil
}
