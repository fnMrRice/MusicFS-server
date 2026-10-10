package sourceaccess

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

type RootConfig struct {
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
	roots  map[string]RootConfig
	mu     sync.Mutex
	grants map[string]grant
}

func New(roots []RootConfig) (*Access, error) {
	a := &Access{roots: make(map[string]RootConfig), grants: make(map[string]grant)}
	for _, cfg := range roots {
		if cfg.ID == "" || !filepath.IsAbs(cfg.Path) {
			return nil, fmt.Errorf("ACCESS_DENIED: root must have an ID and absolute path")
		}
		if _, ok := a.roots[cfg.ID]; ok {
			return nil, fmt.Errorf("ACCESS_DENIED: duplicate root ID")
		}
		r, err := os.OpenRoot(cfg.Path)
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
	r, err := os.OpenRoot(cfg.Path)
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

func (a *Access) open(rootID, path, version string) (*Handle, error) {
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
	h := &Handle{File: f, rootID: rootID, path: path}
	entry, err := describe(f, rootID, path)
	if err != nil {
		f.Close()
		return nil, err
	}
	if version != "" && entry.ObservedVersion != version {
		f.Close()
		return nil, fmt.Errorf("RESOURCE_CHANGED: source version changed")
	}
	h.Entry = entry
	h.checkPath = func() error {
		current, err := a.open(rootID, path, entry.ObservedVersion)
		if err != nil {
			return fmt.Errorf("RESOURCE_CHANGED: source path no longer identifies this version")
		}
		return current.Close()
	}
	return h, nil
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
	f.stop = context.AfterFunc(ctx, func() { f.File.Close() })
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
	Entry        pluginproto.Entry
	rootID, path string
	stop         func() bool
	checkPath    func() error
}

func (h *Handle) Close() error {
	if h.stop != nil {
		h.stop()
	}
	return h.File.Close()
}
func (h *Handle) Validate() error {
	entry, err := describe(h.File, h.rootID, h.path)
	if err != nil {
		return err
	}
	if entry.ObservedVersion != h.Entry.ObservedVersion {
		return fmt.Errorf("RESOURCE_CHANGED: source changed during read")
	}
	return nil
}
