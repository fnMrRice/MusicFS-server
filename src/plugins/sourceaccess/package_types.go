package sourceaccess

import (
	"context"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

const (
	MaxPackageNodes       = 5000
	MaxPackageDepth       = 32
	MaxPackagePathBytes   = 2 << 20
	MaxPackageDirectories = 1000
	PackageReadTimeout    = 30 * time.Second
)

// PackageEntry 的路径仅供宿主使用，对外应由作品服务转换为节点身份及名称。
type PackageEntry struct {
	Path, SourcePath, Name, Kind, MIME, AttachmentKind string
	SizeBytes                                          int64
	Version                                            string
	Entry                                              pluginproto.Entry
}

type PackageTree struct {
	RelativePath, RootIdentity, Signature string
	Entries                               []PackageEntry
}

type PackageDirectory struct {
	Name string `json:"name"`
	Path string `json:"path"`
}
type PackageDirectories struct {
	Path        string             `json:"path"`
	Directories []PackageDirectory `json:"directories"`
}

// ValidPackagePath 不接受清理后才合法的路径，避免不同平台解释出不同范围。
func ValidPackagePath(value string) bool {
	if value == "" || len(value) > 4096 || !utf8.ValidString(value) || !fs.ValidPath(value) || strings.ContainsAny(value, `\:*?<>|"`) {
		return false
	}
	if strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return false
	}
	if value == "." {
		return true
	}
	for _, part := range strings.Split(value, "/") {
		if !filepath.IsLocal(part) || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
	}
	return true
}

func packageFailure(code, message string) error { return domain.Fail(code, message) }

func packageOptions(root, identity string, options pluginproto.ImportOptions) pluginproto.ScanScope {
	return pluginproto.ScanScope{RootID: root, RootIdentity: identity, Recursive: true, ExcludePatterns: options.ExcludePatterns, SkipHidden: options.SkipHidden, RespectNoMedia: options.RespectNoMedia}
}

func packagePathAllowed(r *os.Root, scope pluginproto.ScanScope, value string) error {
	prefixes := []string{"."}
	if value != "." {
		current := ""
		for _, part := range strings.Split(value, "/") {
			current = path.Join(current, part)
			prefixes = append(prefixes, current)
		}
	}
	for _, prefix := range prefixes {
		info, err := r.Lstat(prefix)
		if err != nil {
			return packageFailure("SOURCE_OFFLINE", "Package path is unavailable")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return packageFailure("ACCESS_DENIED", "Linked package paths are not allowed")
		}
		if prefix != "." && pluginproto.Excludes(scope.ExcludePatterns, prefix) {
			return packageFailure("ACCESS_DENIED", "Package path is excluded")
		}
		excluded, err := excludedEntry(r, scope, prefix, info)
		if err != nil {
			return packageFailure("SOURCE_OFFLINE", "Package exclusion state is unavailable")
		}
		if excluded {
			return packageFailure("ACCESS_DENIED", "Package path is excluded")
		}
	}
	return nil
}

func (a *Access) packageRoot(rootID, relative string, options pluginproto.ImportOptions) (*os.Root, pluginproto.ScanScope, error) {
	if !ValidPackagePath(relative) {
		return nil, pluginproto.ScanScope{}, packageFailure("INVALID_ARGUMENT", "Invalid package relative path")
	}
	if err := options.Validate(); err != nil {
		return nil, pluginproto.ScanScope{}, packageFailure("INVALID_ARGUMENT", "Invalid package source options")
	}
	r, err := a.openRoot(rootID)
	if err != nil {
		return nil, pluginproto.ScanScope{}, err
	}
	scope := packageOptions(rootID, a.roots[rootID].ExpectedIdentity, options)
	if err = packagePathAllowed(r, scope, relative); err != nil {
		r.Close()
		return nil, scope, err
	}
	return r, scope, nil
}

// OpenPackageFile 仅重开当前授权根内无链接的固定文件版本。
func (a *Access) OpenPackageFile(ctx context.Context, scope Scope, rootID, relative, version string, options pluginproto.ImportOptions) (*Handle, error) {
	if relative == "." || version == "" {
		return nil, packageFailure("INVALID_ARGUMENT", "A package file and version are required")
	}
	r, _, err := a.packageRoot(rootID, relative, options)
	if err != nil {
		return nil, err
	}
	r.Close()
	resource, err := a.Register(scope, rootID, relative, version, time.Minute)
	if err != nil {
		return nil, err
	}
	h, err := a.Open(ctx, scope, resource.ResourceRef)
	if err != nil {
		a.Revoke(scope.TaskID)
		return nil, err
	}
	check := h.checkPath
	h.checkPath = func() error {
		root, _, err := a.packageRoot(rootID, relative, options)
		if err != nil {
			return err
		}
		root.Close()
		return check()
	}
	if err = h.checkPath(); err != nil {
		h.Close()
		a.Revoke(scope.TaskID)
		return nil, err
	}
	if err = h.VerifyIndexedVersion(); err != nil {
		h.Close()
		a.Revoke(scope.TaskID)
		return nil, err
	}
	return h, nil
}
