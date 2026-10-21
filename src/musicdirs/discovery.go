// Package musicdirs discovers conventional music directories without scanning audio.
package musicdirs

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Default returns the user's conventional Music path without creating it.
func Default() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	path, err := filepath.Abs(filepath.Join(home, "Music"))
	if err != nil {
		return ""
	}
	return path
}

// Find probes only known direct children of the supplied roots.
// Reading at most one entry verifies directory access without scanning music.
func Find(roots []string) []string {
	names := [...]string{"Music", "NeteaseMusic", "CloudMusic", "QQMusic", "Kugou", "KuwoMusic"}
	var paths []string
	seen := map[string]bool{}
	for _, root := range roots {
		for _, name := range names {
			path, err := filepath.Abs(filepath.Join(root, name))
			if err != nil {
				continue
			}
			info, err := os.Stat(path)
			if err != nil || !info.IsDir() {
				continue
			}
			key := canonicalPath(path)
			if runtime.GOOS == "windows" {
				key = strings.ToLower(key)
			}
			if seen[key] {
				continue
			}
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			_, err = f.Readdirnames(1)
			f.Close()
			if err != nil && err != io.EOF {
				continue
			}
			seen[key] = true
			paths = append(paths, path)
		}
	}
	return paths
}

func canonicalPath(path string) string {
	path = filepath.Clean(path)
	var suffix []string
	for current := path; ; current = filepath.Dir(current) {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved
		}
		if filepath.Dir(current) == current {
			return path
		}
		suffix = append(suffix, filepath.Base(current))
	}
}
