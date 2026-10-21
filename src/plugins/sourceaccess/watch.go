package sourceaccess

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

type WatchDirectory struct {
	RelativePath string
	File         *os.File
}

// WatchDirectories returns anchored directory handles. Callers own the handles.
// Registration is bounded, never recursive in the OS, and shares scan scope rules.
func (a *Access) WatchDirectories(ctx context.Context, scope pluginproto.ScanScope, limit int) (result []WatchDirectory, err error) {
	if err = scope.Validate(); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 256 {
		return nil, fmt.Errorf("RATE_LIMITED: watch directory limit reached")
	}
	if root, ok := a.roots[scope.RootID]; !ok || root.ExpectedIdentity != scope.RootIdentity {
		return nil, fmt.Errorf("ACCESS_DENIED: watch root is not authorized")
	}
	r, err := a.openRoot(scope.RootID)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	defer func() {
		if err != nil {
			for _, d := range result {
				d.File.Close()
			}
			result = nil
		}
	}()
	entries := 0
	var visit func(string) error
	visit = func(relative string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !scope.ContainsDirectory(relative) {
			return nil
		}
		before, err := r.Lstat(relative)
		if err != nil {
			return fmt.Errorf("SOURCE_OFFLINE: watch directory unavailable")
		}
		if before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
			return nil
		}
		if scope.SkipHidden && relative != "." && hiddenAttribute(before) {
			return nil
		}
		if len(result) >= limit {
			return fmt.Errorf("RATE_LIMITED: watch directory limit reached")
		}
		f, err := r.Open(relative)
		if err != nil {
			return fmt.Errorf("SOURCE_OFFLINE: watch directory unavailable")
		}
		after, err := f.Stat()
		current, currentErr := r.Lstat(relative)
		if err != nil || currentErr != nil || !os.SameFile(before, after) || !os.SameFile(before, current) || current.Mode()&os.ModeSymlink != 0 {
			f.Close()
			return fmt.Errorf("RESOURCE_CHANGED: watch directory changed")
		}
		result = append(result, WatchDirectory{RelativePath: relative, File: f})
		// Keep the marked directory itself registered so deleting .nomedia can
		// trigger a rescan, but do not register or enumerate its descendants.
		skip, err := excludedEntry(r, scope, relative, before)
		if err != nil {
			return fmt.Errorf("SOURCE_OFFLINE: watch exclusion marker unavailable")
		}
		if skip {
			return nil
		}
		for {
			batch, err := f.ReadDir(128)
			if err != nil && err != io.EOF {
				return fmt.Errorf("SOURCE_OFFLINE: watch directory unreadable")
			}
			for _, entry := range batch {
				entries++
				if entries > 50000 {
					return fmt.Errorf("RATE_LIMITED: watch enumeration limit reached")
				}
				if entry.IsDir() && entry.Type()&os.ModeSymlink == 0 {
					if err := visit(path.Join(relative, entry.Name())); err != nil {
						return err
					}
				}
			}
			if err == io.EOF {
				return nil
			}
			if err := ctx.Err(); err != nil {
				return err
			}
		}
	}
	if err = visit("."); err != nil {
		return result, err
	}
	_, err = a.openRootCheck(scope.RootID)
	return result, err
}
