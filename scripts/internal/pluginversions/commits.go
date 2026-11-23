package pluginversions

import (
	"fmt"
	"iter"
	"slices"
	"strings"
)

type historyEntry struct {
	revision string
	parents  []string
	message  string
	files    []string
}

type changes struct {
	bump   increment
	byFile map[string]increment
}

func (c changes) forFile(file string) increment {
	if c.byFile != nil {
		return c.byFile[file]
	}
	return c.bump
}

// 一次 Git 查询取得一段第一父历史的提交、完整说明及相对第一父的文件差异。
func (r repository) commitHistory(revisions []string) (iter.Seq[historyEntry], error) {
	args := []string{"log", "--reverse", "--first-parent", "--diff-merges=first-parent", "--no-renames", "--format=%x00rev:%H %P%n%B%x00", "--name-only", "-z"}
	args = append(args, revisions...)
	args = append(args, "--", "plugins", manifestPath)
	raw, err := r.git(args...)
	if err != nil {
		return nil, err
	}
	return func(yield func(historyEntry) bool) {
		var entry historyEntry
		for token := range strings.SplitSeq(raw, "\x00") {
			token = strings.TrimPrefix(token, "\n")
			if token == "" {
				continue
			}
			if header, ok := strings.CutPrefix(token, "rev:"); ok {
				if entry.revision != "" && !yield(entry) {
					return
				}
				hashes, message, _ := strings.Cut(header, "\n")
				revision, parents, _ := strings.Cut(hashes, " ")
				entry = historyEntry{revision: revision, parents: strings.Fields(parents), message: message}
			} else {
				entry.files = append(entry.files, token)
			}
		}
		if entry.revision != "" {
			yield(entry)
		}
	}, nil
}

// 显式类型优先。默认合并递归取各侧支实际合入文件的最高类别。
func (r repository) commitChanges(entry historyEntry, shallow []string) (changes, error) {
	bump := classifyCommit(entry.message)
	if bump != unchanged || len(entry.parents) < 2 || commitHeader.MatchString(entry.message) {
		return changes{bump: bump}, nil
	}
	revisions := append([]string{"^" + entry.parents[0]}, entry.parents[1:]...)
	if len(shallow) > 0 {
		// 此处须检查侧支的完整祖先，路径过滤可能隐藏截断点。
		raw, err := r.git(append([]string{"rev-list"}, revisions...)...)
		if err != nil {
			return changes{}, err
		}
		for hash := range strings.FieldsSeq(raw) {
			if slices.Contains(shallow, hash) {
				return changes{}, fmt.Errorf("plugin version history is incomplete for merge %s; fetch its side branches", entry.revision)
			}
		}
	}
	history, err := r.commitHistory(revisions)
	if err != nil {
		return changes{}, err
	}
	result := changes{byFile: map[string]increment{}}
	for child := range history {
		change, err := r.commitChanges(child, nil)
		if err != nil {
			return changes{}, err
		}
		for _, file := range child.files {
			result.byFile[file] = max(result.byFile[file], change.forFile(file))
		}
	}
	return result, nil
}
