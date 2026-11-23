package sourceaccess

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

// PackageSnapshot 保留完整的有限目录树，不受音频扫描的递归开关裁剪。
func (a *Access) PackageSnapshot(ctx context.Context, rootID, relative string, options pluginproto.ImportOptions) (PackageTree, error) {
	ctx, cancel := context.WithTimeout(ctx, PackageReadTimeout)
	defer cancel()
	r, scope, err := a.packageRoot(rootID, relative, options)
	if err != nil {
		return PackageTree{}, err
	}
	defer r.Close()
	tree := PackageTree{RelativePath: relative, RootIdentity: scope.RootIdentity, Entries: []PackageEntry{}}
	visited, pathBytes := 0, 0
	var visit func(string, int) error
	visit = func(local string, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > MaxPackageDepth {
			return packageFailure("RATE_LIMITED", "Package depth limit exceeded")
		}
		source := path.Join(relative, local)
		info, err := r.Lstat(source)
		if err != nil {
			return packageFailure("SOURCE_OFFLINE", "Package entry is unreadable")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return packageFailure("ACCESS_DENIED", "Linked package entries are not allowed")
		}
		if source != "." && pluginproto.Excludes(scope.ExcludePatterns, source) {
			return nil
		}
		excluded, err := excludedEntry(r, scope, source, info)
		if err != nil {
			return packageFailure("SOURCE_OFFLINE", "Package exclusion state is unavailable")
		}
		if excluded {
			return nil
		}
		if !ValidPackagePath(source) {
			return packageFailure("INVALID_ARGUMENT", "Package contains an ambiguous file name")
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return packageFailure("UNSUPPORTED", "Package contains a non-regular entry")
		}
		if local == "." && !info.IsDir() {
			return packageFailure("INVALID_ARGUMENT", "Package path must select a directory")
		}
		pathBytes += len(local) + len(source)
		if len(tree.Entries) >= MaxPackageNodes || pathBytes > MaxPackagePathBytes {
			return packageFailure("RATE_LIMITED", "Package entry budget exceeded")
		}
		f, err := r.Open(source)
		if err != nil {
			return packageFailure("SOURCE_OFFLINE", "Package entry is unreadable")
		}
		defer f.Close()
		actual, err := f.Stat()
		if err != nil || !os.SameFile(info, actual) {
			return packageFailure("RESOURCE_CHANGED", "Package entry changed while opening")
		}
		entry := PackageEntry{Path: local, SourcePath: source, Name: path.Base(source), Kind: "directory"}
		if info.IsDir() {
			entry.Version, err = directoryVersion(f)
			if err != nil {
				return err
			}
			tree.Entries = append(tree.Entries, entry)
			children := []string{}
			for {
				if err := ctx.Err(); err != nil {
					return err
				}
				batch, err := f.ReadDir(128)
				if err != nil && err != io.EOF {
					return packageFailure("SOURCE_OFFLINE", "Package directory is unreadable")
				}
				visited += len(batch)
				if visited >= MaxPackageNodes {
					return packageFailure("RATE_LIMITED", "Package enumeration limit exceeded")
				}
				for _, child := range batch {
					children = append(children, child.Name())
				}
				if err == io.EOF {
					break
				}
			}
			sort.Strings(children)
			for _, child := range children {
				if err = visit(path.Join(local, child), depth+1); err != nil {
					return err
				}
			}
			version, err := directoryVersion(f)
			if err != nil || version != entry.Version {
				return packageFailure("RESOURCE_CHANGED", "Package directory changed during enumeration")
			}
		} else {
			entry.Entry, err = describe(f, rootID, source, a.formats)
			if err != nil {
				return packageFailure("SOURCE_OFFLINE", "Package file could not be inspected")
			}
			entry.SizeBytes, entry.Version, entry.MIME = entry.Entry.SizeBytes, entry.Entry.ObservedVersion, entry.Entry.MIME
			_, audio, err := a.formats.Detect(f, entry.SizeBytes)
			if err != nil {
				return packageFailure("SOURCE_OFFLINE", "Package format detection failed")
			}
			if audio {
				entry.Kind = "audio"
			} else {
				entry.Kind = "attachment"
				entry.MIME, entry.AttachmentKind, err = packageAttachment(f, entry.SizeBytes)
				if err != nil {
					return err
				}
			}
			current, err := describe(f, rootID, source, a.formats)
			if err != nil || current.ObservedVersion != entry.Version {
				return packageFailure("RESOURCE_CHANGED", "Package file changed during inspection")
			}
			tree.Entries = append(tree.Entries, entry)
		}
		current, err := r.Lstat(source)
		if err != nil || current.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, current) {
			return packageFailure("RESOURCE_CHANGED", "Package path changed during enumeration")
		}
		return nil
	}
	if err = visit(".", 0); err != nil {
		return PackageTree{}, err
	}
	if _, err = a.openRootCheck(rootID); err != nil {
		return PackageTree{}, err
	}
	if err = ctx.Err(); err != nil {
		return PackageTree{}, err
	}
	raw, err := json.Marshal(tree)
	if err != nil {
		return PackageTree{}, err
	}
	tree.Signature = fmt.Sprintf("%x", sha256.Sum256(raw))
	return tree, nil
}

func directoryVersion(f *os.File) (string, error) {
	info, err := f.Stat()
	if err != nil || !info.IsDir() {
		return "", packageFailure("RESOURCE_CHANGED", "Package directory changed")
	}
	identity, err := fileIdentity(f)
	if err != nil {
		return "", packageFailure("SOURCE_OFFLINE", "Package directory identity is unavailable")
	}
	version, err := ObservedVersion(f, info, identity)
	if err != nil {
		return "", packageFailure("SOURCE_OFFLINE", "Package directory version is unavailable")
	}
	return version, nil
}

func packageAttachment(f *os.File, size int64) (string, string, error) {
	header := make([]byte, min(size, 512))
	if len(header) > 0 {
		n, err := f.ReadAt(header, 0)
		if err != nil && err != io.EOF || n != len(header) {
			return "", "", packageFailure("RESOURCE_CHANGED", "Package attachment changed while reading")
		}
	}
	mime := http.DetectContentType(header)
	switch mime {
	case "image/png", "image/jpeg", "image/gif":
		return mime, "image", nil
	}
	if strings.HasPrefix(mime, "text/plain") {
		return "text/plain; charset=utf-8", "text", nil
	}
	return "application/octet-stream", "download", nil
}
