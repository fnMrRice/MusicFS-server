package library

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

type PackageEntry = sourceaccess.PackageEntry
type PackageDirectory = sourceaccess.PackageDirectory
type PackageDirectories = sourceaccess.PackageDirectories

// PackageSnapshot 仅供宿主持久化和核对；公开接口必须另行转换相对源路径。
type PackageSnapshot struct {
	ProviderID, RootID, RelativePath, RootIdentity, ConfigRevision, Signature string
	Entries                                                                   []PackageEntry
}

func (s *Service) packageLease(ctx context.Context, owner, provider string) (*ReadLease, error) {
	lease, err := s.HoldRead(ctx, owner, provider)
	if err != nil {
		return nil, packageSourceError(err)
	}
	w := lease.runtime.Worker
	if w == nil || w.Access == nil {
		lease.Close()
		return nil, domain.Fail("SOURCE_OFFLINE", "Package provider is offline")
	}
	if w.Manifest.PluginID != "musicfs.plugin.source.localfiles" {
		lease.Close()
		return nil, domain.Fail("UNSUPPORTED", "Package imports require Local Files")
	}
	select {
	case <-w.Done():
		lease.Close()
		return nil, domain.Fail("SOURCE_OFFLINE", "Package provider is offline")
	default:
	}
	return lease, nil
}

func packageImportOptions(r Runtime, root string) pluginproto.ImportOptions {
	if options, ok := r.RootImportDefaults[root]; ok {
		return options
	}
	return r.ImportDefaults
}

func (s *Service) BrowsePackageDirectories(ctx context.Context, owner, provider, root, path string) (PackageDirectories, error) {
	lease, err := s.packageLease(ctx, owner, provider)
	if err != nil {
		return PackageDirectories{}, err
	}
	defer lease.Close()
	r := lease.runtime
	result, err := r.Worker.Access.PackageDirectories(lease.Context(), root, path, packageImportOptions(r, root))
	return result, packageSourceError(err)
}

func (s *Service) PackageSourceSnapshot(ctx context.Context, owner, provider, root, path string) (PackageSnapshot, error) {
	lease, err := s.packageLease(ctx, owner, provider)
	if err != nil {
		return PackageSnapshot{}, err
	}
	defer lease.Close()
	return packageSnapshot(lease.Context(), lease.runtime, root, path)
}

func packageSnapshot(ctx context.Context, r Runtime, root, path string) (PackageSnapshot, error) {
	tree, err := r.Worker.Access.PackageSnapshot(ctx, root, path, packageImportOptions(r, root))
	if err != nil {
		return PackageSnapshot{}, packageSourceError(err)
	}
	result := PackageSnapshot{ProviderID: r.ID, RootID: root, RelativePath: tree.RelativePath, RootIdentity: tree.RootIdentity, ConfigRevision: r.ConfigRevision, Entries: tree.Entries}
	if path == "." && len(result.Entries) > 0 {
		name := r.RootNames[root]
		if name == "" {
			name = root
		}
		result.Entries[0].Name = name
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return PackageSnapshot{}, domain.Fail("TEMPORARY_FAILURE", "Package snapshot could not be encoded")
	}
	result.Signature = fmt.Sprintf("%x", sha256.Sum256(raw))
	return result, nil
}

// PackageSourceOpen 的调用方必须执行 cleanup，直到所有读取与稳定性核对完成。
func (s *Service) PackageSourceOpen(ctx context.Context, owner, provider, root, path, version string) (*sourceaccess.StableReader, func(), error) {
	lease, err := s.packageLease(ctx, owner, provider)
	if err != nil {
		return nil, nil, err
	}
	r := lease.runtime
	task := domain.NewID()
	scope := sourceaccess.Scope{UserID: owner, InstanceID: provider, TaskID: task, AssetID: version}
	h, err := r.Worker.Access.OpenPackageFile(lease.Context(), scope, root, path, version, packageImportOptions(r, root))
	if err != nil {
		r.Worker.Access.Revoke(task)
		lease.Close()
		return nil, nil, packageSourceError(err)
	}
	stable, err := h.Stable(lease.Context())
	if err != nil {
		h.Close()
		r.Worker.Access.Revoke(task)
		lease.Close()
		return nil, nil, packageSourceError(err)
	}
	var once sync.Once
	cleanup := func() { once.Do(func() { stable.Close(); h.Close(); r.Worker.Access.Revoke(task); lease.Close() }) }
	return stable, cleanup, nil
}

// 底层文件系统错误和插件错误可能带物理路径，此边界只保留已知错误码。
func packageSourceError(err error) error {
	if err == nil {
		return nil
	}
	code := "TEMPORARY_FAILURE"
	var failure *domain.Error
	if errors.As(err, &failure) {
		code = failure.Code
	} else {
		code = strings.SplitN(err.Error(), ":", 2)[0]
	}
	if errors.Is(err, context.Canceled) {
		code = "CANCELED"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		code = "TEMPORARY_FAILURE"
	}
	switch code {
	case "INVALID_ARGUMENT", "ACCESS_DENIED", "NOT_FOUND", "SOURCE_OFFLINE", "RESOURCE_CHANGED", "UNSUPPORTED", "CANCELED", "RATE_LIMITED", "CONFLICT", "PLUGIN_DISABLED", "PROTOCOL_MISMATCH":
	default:
		code = "TEMPORARY_FAILURE"
	}
	return domain.Fail(code, "Package source operation failed")
}
