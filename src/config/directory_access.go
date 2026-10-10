package config

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

// ValidateDirectoryPath rejects ambiguous user-supplied filesystem syntax before
// normalization. Deployment permissions remain the only authority to open it.
func ValidateDirectoryPath(path string) error {
	invalid := len(path) == 0 || len(path) > 4096 || !utf8.ValidString(path) || !filepath.IsAbs(path)
	for _, r := range path {
		invalid = invalid || unicode.IsControl(r)
	}
	parts := strings.Split(path, string(filepath.Separator))
	if runtime.GOOS == "windows" {
		path = strings.ReplaceAll(path, "/", `\`)
		volume := filepath.VolumeName(path)
		drive := len(volume) == 2 && volume[1] == ':' && strings.ContainsAny(volume[:1], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
		unc := strings.HasPrefix(volume, `\\`) && !strings.HasPrefix(volume, `\\?\`) && !strings.HasPrefix(volume, `\\.\`)
		invalid = invalid || !drive && !unc
		parts = strings.Split(strings.TrimPrefix(path, volume), `\`)
		if unc {
			parts = append(parts, strings.Split(strings.TrimPrefix(volume, `\\`), `\`)...)
		}
		for _, part := range parts {
			invalid = invalid || strings.ContainsAny(part, `:<>"|?*`) || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ")
			if part != "" && !filepath.IsLocal(part) {
				invalid = true
			}
		}
	}
	for _, part := range parts {
		invalid = invalid || part == "." || part == ".."
	}
	if invalid {
		return domain.Fail("INVALID_ARGUMENT", "An unambiguous absolute directory path is required")
	}
	return nil
}

// DirectoryBoundary checks lexical authority without following untrusted links.
// OpenAllowedDirectory performs the actual access through a restricted handle.
func DirectoryBoundary(i Instance, path string) (string, string, error) {
	if err := ValidateDirectoryPath(path); err != nil {
		return "", "", err
	}
	boundary, relative := "", ""
	for _, allowed := range AllowedDirectories(i) {
		allowed = filepath.Clean(allowed)
		rel, err := filepath.Rel(allowed, path)
		if err == nil && (rel == "." || filepath.IsLocal(rel)) && len(allowed) > len(boundary) {
			boundary, relative = allowed, rel
		}
	}
	if boundary != "" {
		return boundary, relative, nil
	}
	return "", "", domain.Fail("ACCESS_DENIED", "Music directory is outside deployment permissions")
}

func OpenAllowedDirectory(i Instance, path string) (*os.Root, error) {
	boundary, rel, err := DirectoryBoundary(i, path)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(boundary)
	if err != nil {
		return nil, domain.Fail("SOURCE_UNAVAILABLE", "Music directory is unavailable")
	}
	defer root.Close()
	selected, err := root.OpenRoot(rel)
	if err != nil {
		return nil, domain.Fail("SOURCE_UNAVAILABLE", "Music directory is unavailable or escapes its allowed root")
	}
	file, err := selected.Open(".")
	if err == nil {
		_, err = file.ReadDir(1)
		file.Close()
	}
	if err != nil && err != io.EOF {
		selected.Close()
		return nil, domain.Fail("SOURCE_UNAVAILABLE", "Music directory is unreadable")
	}
	return selected, nil
}

// ConfiguredDirectoryIDs resolves selected roots once per browser request. The
// returned lookup is a UI identity hint; it never grants filesystem authority.
func ConfiguredDirectoryIDs(i Instance) func(string) *string {
	ids := map[string]string{}
	for _, root := range i.Config.MusicRoots {
		ids[directoryIdentity(root.Path)] = root.ID
	}
	return func(path string) *string {
		if id, ok := ids[directoryIdentity(path)]; ok {
			return &id
		}
		return nil
	}
}

func directoryIdentity(path string) string {
	path = canonicalPath(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}
