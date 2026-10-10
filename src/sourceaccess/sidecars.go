package sourceaccess

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

// Sidecar is an exact sibling grant. Paths stay in the host and must not be
// serialized into scraper DTOs or passed through to a client.
type Sidecar struct {
	Resource    pluginproto.Resource
	Kind, Match string
}

// AuthorizeSidecars derives fixed sibling names from an authorized current
// source. It never accepts a plugin-supplied path or walks a directory tree.
func (a *Access) AuthorizeSidecars(ctx context.Context, scope Scope, rootID, audioPath, version string, fields []string) ([]Sidecar, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if scope.AssetID == "" || version == "" || len(fields) < 1 || len(fields) > 2 {
		return nil, fmt.Errorf("ACCESS_DENIED: incomplete sidecar scope")
	}
	for _, field := range fields {
		if field != "lyrics" && field != "artwork" {
			return nil, fmt.Errorf("ACCESS_DENIED: invalid sidecar field")
		}
	}
	source, err := a.open(rootID, audioPath, version)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	root, err := a.openRoot(rootID)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if err = rejectSidecarLinks(root, audioPath); err != nil {
		return nil, err
	}
	dir, base := filepath.Split(filepath.FromSlash(audioPath))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	type requested struct{ name, kind, match string }
	names := []requested{}
	if slices.Contains(fields, "lyrics") {
		for _, ext := range []string{".lrc", ".txt"} {
			names = append(names, requested{stem + ext, "lyrics", "exactStem"})
		}
	}
	if slices.Contains(fields, "artwork") {
		for _, ext := range []string{".jpg", ".jpeg", ".png"} {
			names = append(names, requested{stem + ext, "artwork", "exactStem"})
		}
		for _, name := range []string{"cover.jpg", "cover.png", "folder.jpg", "folder.png"} {
			names = append(names, requested{name, "artwork", "directoryCover"})
		}
	}
	result := []Sidecar{}
	seen := map[string]bool{}
	for _, item := range names {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := filepath.ToSlash(filepath.Join(dir, item.name))
		key := path
		if seen[key] {
			continue
		}
		seen[key] = true
		if err = rejectSidecarLinks(root, path); err != nil {
			continue
		}
		entry, err := a.Inspect(rootID, path)
		if err != nil {
			continue
		}
		limit := int64(1 << 20)
		if item.kind == "artwork" {
			limit = 8 << 20
		}
		if entry.SizeBytes <= 0 || entry.SizeBytes > limit {
			continue
		}
		resource, err := a.Register(scope, rootID, path, entry.ObservedVersion, 30*time.Second)
		if err != nil {
			return nil, err
		}
		result = append(result, Sidecar{Resource: resource, Kind: item.kind, Match: item.match})
	}
	if err = source.Validate(); err != nil {
		return nil, err
	}
	if err = source.checkPath(); err != nil {
		return nil, err
	}
	return result, nil
}

func rejectSidecarLinks(root *os.Root, path string) error {
	if !filepath.IsLocal(path) || path == "." {
		return fmt.Errorf("ACCESS_DENIED: invalid sidecar relative path")
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	prefix := ""
	for _, part := range parts {
		prefix = filepath.Join(prefix, part)
		info, err := root.Lstat(prefix)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("ACCESS_DENIED: linked sidecar path refused")
		}
	}
	return nil
}
