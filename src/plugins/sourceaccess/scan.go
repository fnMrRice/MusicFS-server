package sourceaccess

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func describe(f *os.File, rootID, path string, formats *audioformat.Registry) (pluginproto.Entry, error) {
	return describeFile(f, rootID, path, formats, "")
}

func describeFile(f *os.File, rootID, path string, formats *audioformat.Registry, indexedVersion string) (pluginproto.Entry, error) {
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return pluginproto.Entry{}, fmt.Errorf("ACCESS_DENIED: source is not a regular file")
	}
	identity, err := fileIdentity(f)
	if err != nil {
		return pluginproto.Entry{}, fmt.Errorf("SOURCE_OFFLINE: file identity unavailable")
	}
	version := indexedVersion
	if version == "" {
		version, err = ObservedVersion(f, info, identity)
		if err != nil {
			return pluginproto.Entry{}, fmt.Errorf("SOURCE_OFFLINE: file version unavailable")
		}
	}
	links, err := fileLinkCount(f)
	if err != nil || links == 0 {
		return pluginproto.Entry{}, fmt.Errorf("SOURCE_OFFLINE: file link information unavailable")
	}
	entry := pluginproto.Entry{RootID: rootID, RelativePath: filepath.ToSlash(path), FileIdentity: identity, LinkCount: links, ObservedVersion: version, SizeBytes: info.Size(), MIME: "application/octet-stream"}
	match, ok, err := formats.Detect(f, info.Size())
	if err != nil {
		return pluginproto.Entry{}, err
	}
	if ok {
		entry.MIME = match.MIME
		entry.DurationMs = match.DurationMs
	}
	return entry, nil
}

func (a *Access) Entries(ctx context.Context, scope pluginproto.ScanScope) ([]pluginproto.Entry, string, []string, error) {
	return a.entries(ctx, scope, nil)
}

func (a *Access) EntriesWithExclusions(ctx context.Context, scope pluginproto.ScanScope) (entries []pluginproto.Entry, identity string, issues, excluded []string, err error) {
	entries, identity, issues, err = a.entries(ctx, scope, &excluded)
	return
}

func (a *Access) entries(ctx context.Context, scope pluginproto.ScanScope, excluded *[]string) ([]pluginproto.Entry, string, []string, error) {
	if err := scope.Validate(); err != nil {
		return nil, "", nil, err
	}
	rootID := scope.RootID
	if root, ok := a.roots[rootID]; !ok || root.ExpectedIdentity != scope.RootIdentity {
		return nil, "", nil, fmt.Errorf("ACCESS_DENIED: scan root is not authorized")
	}
	r, err := a.openRoot(rootID)
	if err != nil {
		return nil, "", nil, err
	}
	defer r.Close()
	entries := make([]pluginproto.Entry, 0)
	issues := make([]string, 0)
	excludedCount, excludedBytes := 0, 0
	err = fs.WalkDir(r.FS(), ".", func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if len(issues) < 100 {
				issues = append(issues, "unreadable branch")
			}
			return nil
		}
		// 先裁剪未选择的深度和静态排除项，再访问范围内的属性与标记文件。
		if d.IsDir() && !scope.ContainsDirectory(path) {
			return fs.SkipDir
		}
		if path != "." && pluginproto.Excludes(scope.ExcludePatterns, path) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if scope.SkipHidden || scope.RespectNoMedia {
			info, err := r.Lstat(path)
			if err != nil {
				return fmt.Errorf("RESOURCE_CHANGED: scan entry attributes unavailable")
			}
			skip, err := excludedEntry(r, scope, path, info)
			if err != nil {
				return fmt.Errorf("SOURCE_OFFLINE: scan exclusion marker unavailable")
			}
			if skip {
				excludedCount++
				excludedBytes += len(path)
				if excludedCount > 50000 || excludedBytes > 8<<20 {
					return fmt.Errorf("RATE_LIMITED: scan exclusion limit reached")
				}
				if excluded != nil {
					*excluded = append(*excluded, path)
				}
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
		}
		if d.Type()&os.ModeSymlink != 0 {
			if len(issues) < 100 {
				issues = append(issues, "symbolic link skipped")
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if !scope.Contains(rootID, path) {
			return nil
		}
		// 扩展名仅筛选插件声明的候选，避免备份或临时副本成为新来源。
		// 候选的实际格式仍完全根据插件对文件内容的识别结果决定。
		if !a.formats.SupportsExtension(path) {
			return nil
		}
		if len(entries) >= 50000 {
			return fmt.Errorf("RATE_LIMITED: scan entry limit reached")
		}
		f, err := r.Open(path)
		if err != nil {
			if len(issues) < 100 {
				issues = append(issues, "unreadable file")
			}
			return nil
		}
		entry, err := describe(f, rootID, path, a.formats)
		f.Close()
		if err != nil {
			if len(issues) < 100 {
				issues = append(issues, "invalid file")
			}
			return nil
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil && err != io.EOF {
		return nil, "", issues, err
	}
	if _, err = a.openRootCheck(rootID); err != nil {
		return nil, "", issues, err
	}
	return entries, a.roots[rootID].ExpectedIdentity, issues, nil
}

func (a *Access) openRootCheck(rootID string) (bool, error) {
	r, err := a.openRoot(rootID)
	if err != nil {
		return false, err
	}
	r.Close()
	return true, nil
}
