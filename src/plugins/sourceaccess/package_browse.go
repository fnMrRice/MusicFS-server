package sourceaccess

import (
	"context"
	"io"
	"os"
	"path"
	"sort"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func (a *Access) PackageDirectories(ctx context.Context, rootID, relative string, options pluginproto.ImportOptions) (PackageDirectories, error) {
	ctx, cancel := context.WithTimeout(ctx, PackageReadTimeout)
	defer cancel()
	r, scope, err := a.packageRoot(rootID, relative, options)
	if err != nil {
		return PackageDirectories{}, err
	}
	defer r.Close()
	f, err := r.Open(relative)
	if err != nil {
		return PackageDirectories{}, packageFailure("SOURCE_OFFLINE", "Package directory is unavailable")
	}
	defer f.Close()
	original, err := f.Stat()
	if err != nil || !original.IsDir() {
		return PackageDirectories{}, packageFailure("INVALID_ARGUMENT", "Package path must select a directory")
	}
	version, err := directoryVersion(f)
	if err != nil {
		return PackageDirectories{}, err
	}
	result := PackageDirectories{Path: relative, Directories: []PackageDirectory{}}
	count := 0
	for {
		if err = ctx.Err(); err != nil {
			return PackageDirectories{}, err
		}
		entries, readErr := f.ReadDir(128)
		if readErr != nil && readErr != io.EOF {
			return PackageDirectories{}, packageFailure("SOURCE_OFFLINE", "Package directory is unreadable")
		}
		count += len(entries)
		if count > MaxPackageNodes {
			return PackageDirectories{}, packageFailure("RATE_LIMITED", "Directory enumeration limit exceeded")
		}
		for _, entry := range entries {
			child := path.Join(relative, entry.Name())
			info, err := r.Lstat(child)
			if err != nil {
				return PackageDirectories{}, packageFailure("RESOURCE_CHANGED", "Package directory entry changed")
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return PackageDirectories{}, packageFailure("ACCESS_DENIED", "Linked package entries are not allowed")
			}
			if !info.IsDir() {
				continue
			}
			if !ValidPackagePath(child) {
				return PackageDirectories{}, packageFailure("INVALID_ARGUMENT", "Package contains an ambiguous directory name")
			}
			if pluginproto.Excludes(scope.ExcludePatterns, child) {
				continue
			}
			excluded, err := excludedEntry(r, scope, child, info)
			if err != nil {
				return PackageDirectories{}, packageFailure("SOURCE_OFFLINE", "Package exclusion state is unavailable")
			}
			if excluded {
				continue
			}
			if len(result.Directories) >= MaxPackageDirectories {
				return PackageDirectories{}, packageFailure("RATE_LIMITED", "Directory count exceeds limit")
			}
			result.Directories = append(result.Directories, PackageDirectory{Name: entry.Name(), Path: child})
		}
		if readErr == io.EOF {
			break
		}
	}
	current, err := directoryVersion(f)
	if err != nil || current != version {
		return PackageDirectories{}, packageFailure("RESOURCE_CHANGED", "Package directory changed during enumeration")
	}
	if err = packagePathAllowed(r, scope, relative); err != nil {
		return PackageDirectories{}, err
	}
	actual, err := r.Lstat(relative)
	if err != nil || !os.SameFile(original, actual) {
		return PackageDirectories{}, packageFailure("RESOURCE_CHANGED", "Package directory was replaced during enumeration")
	}
	if _, err = a.openRootCheck(rootID); err != nil {
		return PackageDirectories{}, err
	}
	if err = ctx.Err(); err != nil {
		return PackageDirectories{}, err
	}
	sort.Slice(result.Directories, func(i, j int) bool { return result.Directories[i].Name < result.Directories[j].Name })
	return result, nil
}
