package sourceaccess

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audiocodecs"
)

type RootConfig struct {
	Boundary         string `json:"-"`
	ID               string `json:"id"`
	Path             string `json:"path"`
	ExpectedIdentity string `json:"expectedIdentity"`
}
type Scope struct{ UserID, InstanceID, TaskID, AssetID string }
type grant struct {
	scope                 Scope
	rootID, path, version string
	expires               time.Time
}
type Access struct {
	roots   map[string]RootConfig
	mu      sync.Mutex
	grants  map[string]grant
	formats *audioformat.Registry
}

func New(roots []RootConfig) (*Access, error) {
	return NewWithFormats(roots, audiocodecs.DefaultRegistry())
}

// NewWithFormats 为当前来源注入插件识别注册表，避免扫描器固定支持的格式。
func NewWithFormats(roots []RootConfig, formats *audioformat.Registry) (*Access, error) {
	a := &Access{roots: make(map[string]RootConfig), grants: make(map[string]grant), formats: formats}
	for _, cfg := range roots {
		if cfg.ID == "" || !filepath.IsAbs(cfg.Path) {
			return nil, fmt.Errorf("ACCESS_DENIED: root must have an ID and absolute path")
		}
		if _, ok := a.roots[cfg.ID]; ok {
			return nil, fmt.Errorf("ACCESS_DENIED: duplicate root ID")
		}
		r, err := openConfiguredRoot(cfg)
		if err != nil {
			return nil, fmt.Errorf("SOURCE_OFFLINE: root unavailable")
		}
		f, err := r.Open(".")
		if err != nil {
			r.Close()
			return nil, fmt.Errorf("SOURCE_OFFLINE: root identity unavailable")
		}
		identity, err := fileIdentity(f)
		f.Close()
		r.Close()
		if err != nil {
			return nil, fmt.Errorf("SOURCE_OFFLINE: root identity unavailable")
		}
		if cfg.ExpectedIdentity != "" && cfg.ExpectedIdentity != identity {
			return nil, fmt.Errorf("SOURCE_OFFLINE: root identity mismatch")
		}
		cfg.ExpectedIdentity = identity
		a.roots[cfg.ID] = cfg
	}
	return a, nil
}

func (a *Access) Roots() []RootConfig {
	result := make([]RootConfig, 0, len(a.roots))
	for _, root := range a.roots {
		result = append(result, root)
	}
	return result
}

func (a *Access) openRoot(id string) (*os.Root, error) {
	cfg, ok := a.roots[id]
	if !ok {
		return nil, fmt.Errorf("ACCESS_DENIED: unknown root")
	}
	r, err := openConfiguredRoot(cfg)
	if err != nil {
		return nil, fmt.Errorf("SOURCE_OFFLINE: root unavailable")
	}
	f, err := r.Open(".")
	if err != nil {
		r.Close()
		return nil, fmt.Errorf("SOURCE_OFFLINE: root unavailable")
	}
	identity, err := fileIdentity(f)
	f.Close()
	if err != nil || identity != cfg.ExpectedIdentity {
		r.Close()
		return nil, fmt.Errorf("SOURCE_OFFLINE: root identity changed")
	}
	return r, nil
}

func (a *Access) openPath(rootID, path string) (*os.File, error) {
	if !filepath.IsLocal(path) || path == "." {
		return nil, fmt.Errorf("ACCESS_DENIED: invalid relative path")
	}
	r, err := a.openRoot(rootID)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := r.Open(filepath.FromSlash(path))
	if err != nil {
		return nil, fmt.Errorf("NOT_FOUND: source is unavailable")
	}
	return f, nil
}

func (a *Access) openFile(rootID, path, indexedVersion string) (*Handle, error) {
	f, err := a.openPath(rootID, path)
	if err != nil {
		return nil, err
	}
	h := &Handle{File: f, rootID: rootID, path: path, formats: a.formats}
	entry, err := describeFile(f, rootID, path, a.formats, indexedVersion)
	if err != nil {
		f.Close()
		return nil, err
	}
	h.Entry = entry
	return h, nil
}

func (a *Access) open(rootID, path, version string) (*Handle, error) {
	h, err := a.openFile(rootID, path, "")
	if err != nil {
		return nil, err
	}
	if version != "" && h.Entry.ObservedVersion != version {
		h.Close()
		return nil, fmt.Errorf("RESOURCE_CHANGED: source version changed")
	}
	identity := h.Entry.FileIdentity
	h.checkPath = func() error {
		return a.checkFileIdentity(rootID, path, identity)
	}
	return h, nil
}

// 重新从授权根打开路径，只比较文件身份。内容版本由持有该文件的读取方校验。
func (a *Access) checkFileIdentity(rootID, path, identity string) error {
	current, err := a.openPath(rootID, path)
	if err != nil {
		return fmt.Errorf("RESOURCE_CHANGED: source path is unavailable")
	}
	defer current.Close()
	currentID, err := fileIdentity(current)
	if err != nil || currentID != identity {
		return fmt.Errorf("RESOURCE_CHANGED: protected source path was replaced")
	}
	return nil
}

// OpenIndexed 在授权根内打开当前普通文件，保留索引版本供调用方关联曲目。
// 这种读取不验证索引与内容是否一致，也不能用于发布资产证据。
func (a *Access) OpenIndexed(ctx context.Context, scope Scope, rootID, path, indexedVersion string) (*Handle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if scope.UserID == "" || scope.InstanceID == "" || scope.TaskID == "" || indexedVersion == "" {
		return nil, fmt.Errorf("ACCESS_DENIED: invalid indexed source scope")
	}
	h, err := a.openFile(rootID, path, indexedVersion)
	if err != nil {
		return nil, err
	}
	h.stop = context.AfterFunc(ctx, func() { h.closeFile() })
	return h, nil
}

// CheckIndexedFile 确认索引路径仍可访问。Windows 曲目详情使用最近一次扫描的版本。
func (a *Access) CheckIndexedFile(rootID, path, version string) error {
	if runtime.GOOS == "windows" {
		f, err := a.openPath(rootID, path)
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("ACCESS_DENIED: source is not a regular file")
		}
		return nil
	}
	h, err := a.open(rootID, path, version)
	if err != nil {
		return err
	}
	return h.Close()
}

func (a *Access) Inspect(rootID, path string) (pluginproto.Entry, error) {
	f, err := a.open(rootID, path, "")
	if err != nil {
		return pluginproto.Entry{}, err
	}
	defer f.Close()
	return f.Entry, nil
}

func (a *Access) Register(scope Scope, rootID, path, version string, ttl time.Duration) (pluginproto.Resource, error) {
	if scope.UserID == "" || scope.InstanceID == "" || scope.TaskID == "" || ttl <= 0 || ttl > time.Hour {
		return pluginproto.Resource{}, fmt.Errorf("ACCESS_DENIED: invalid resource scope")
	}
	f, err := a.open(rootID, path, version)
	if err != nil {
		return pluginproto.Resource{}, err
	}
	defer f.Close()
	var random [24]byte
	if _, err = rand.Read(random[:]); err != nil {
		return pluginproto.Resource{}, err
	}
	id := hex.EncodeToString(random[:])
	a.mu.Lock()
	defer a.mu.Unlock()
	for ref, g := range a.grants {
		if time.Now().After(g.expires) {
			delete(a.grants, ref)
		}
	}
	if len(a.grants) >= 4096 {
		return pluginproto.Resource{}, fmt.Errorf("RATE_LIMITED: resource limit reached")
	}
	a.grants[id] = grant{scope: scope, rootID: rootID, path: path, version: f.Entry.ObservedVersion, expires: time.Now().Add(ttl)}
	return pluginproto.Resource{ResourceRef: id, RootID: rootID, RelativePath: path, ObservedVersion: f.Entry.ObservedVersion, SizeBytes: f.Entry.SizeBytes, MIME: f.Entry.MIME, Range: true}, nil
}

func (a *Access) Open(ctx context.Context, scope Scope, ref string) (*Handle, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	g, ok := a.grants[ref]
	a.mu.Unlock()
	if !ok || g.scope != scope || time.Now().After(g.expires) {
		return nil, fmt.Errorf("ACCESS_DENIED: resource unavailable")
	}
	f, err := a.open(g.rootID, g.path, g.version)
	if err != nil {
		return nil, err
	}
	f.stop = context.AfterFunc(ctx, func() { f.closeFile() })
	return f, nil
}

func (a *Access) Revoke(taskID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for ref, g := range a.grants {
		if g.scope.TaskID == taskID {
			delete(a.grants, ref)
		}
	}
}

type Handle struct {
	*os.File
	metadataMu   sync.Mutex
	Entry        pluginproto.Entry
	rootID, path string
	stop         func() bool
	checkPath    func() error
	formats      *audioformat.Registry
}

func (h *Handle) Close() error {
	if h.stop != nil {
		h.stop()
	}
	return h.closeFile()
}
func (h *Handle) closeFile() error {
	h.metadataMu.Lock()
	defer h.metadataMu.Unlock()
	return h.File.Close()
}

// Windows File.Stat reads the native handle without the I/O lock used by Read.
// Serialize metadata access with asynchronous cancellation/Close.
func (h *Handle) Stat() (os.FileInfo, error) {
	h.metadataMu.Lock()
	defer h.metadataMu.Unlock()
	return h.File.Stat()
}
func (h *Handle) Validate() error {
	if h.checkPath == nil {
		_, err := h.Stat()
		return err
	}
	return h.VerifyIndexedVersion()
}

// VerifyIndexedVersion 在发布扫描探测结果前核对同一个文件句柄，避免探测期间写入了新内容。
func (h *Handle) VerifyIndexedVersion() error {
	h.metadataMu.Lock()
	defer h.metadataMu.Unlock()
	entry, err := describe(h.File, h.rootID, h.path, h.formats)
	if err != nil {
		return err
	}
	if entry.ObservedVersion != h.Entry.ObservedVersion {
		return fmt.Errorf("RESOURCE_CHANGED: source changed during read")
	}
	return nil
}
