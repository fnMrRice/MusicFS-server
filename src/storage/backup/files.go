package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type contextReader struct {
	context.Context
	io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}
func safeName(name string) bool {
	if !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, "\\:") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if strings.TrimRight(part, ". ") != part {
			return false
		}
	}
	return true
}
func regular(root *os.Root, name string) (*os.File, error) {
	if !safeName(name) {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid backup relative path")
	}
	for current := name; current != "."; current = path.Dir(current) {
		info, err := root.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, domain.Fail("ACCESS_DENIED", "Backup symbolic links are unsupported")
		}
		if current == name && !info.Mode().IsRegular() {
			return nil, domain.Fail("ACCESS_DENIED", "Backup content must be regular files")
		}
	}
	return root.Open(name)
}
func copyFile(ctx context.Context, input io.Reader, root *os.Root, name string, limit int64) (File, error) {
	var result File
	if !safeName(name) {
		return result, domain.Fail("INVALID_ARGUMENT", "Invalid backup relative path")
	}
	if err := root.MkdirAll(path.Dir(name), 0700); err != nil {
		return result, err
	}
	out, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return result, err
	}
	defer out.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), io.LimitReader(contextReader{ctx, input}, limit+1))
	if err != nil {
		return result, err
	}
	if n > limit {
		return result, domain.Fail("RATE_LIMITED", "Backup file exceeds its size limit")
	}
	if err = out.Sync(); err != nil {
		return result, err
	}
	if err = out.Close(); err != nil {
		return result, err
	}
	return File{Path: name, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}
func checksumFile(ctx context.Context, root *os.Root, name string, limit int64) (File, error) {
	in, err := regular(root, name)
	if err != nil {
		return File{}, err
	}
	defer in.Close()
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(contextReader{ctx, in}, limit+1))
	if err != nil {
		return File{}, err
	}
	if n > limit {
		return File{}, domain.Fail("RATE_LIMITED", "Backup content exceeds its size limit")
	}
	return File{Path: name, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}
func jsonFile(ctx context.Context, root *os.Root, name string, value any) (File, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return File{}, err
	}
	return copyFile(ctx, strings.NewReader(string(raw)+"\n"), root, name, maxManifestBytes)
}
func readJSON(root *os.Root, name string, value any) error {
	in, err := regular(root, name)
	if err != nil {
		return err
	}
	defer in.Close()
	reader := json.NewDecoder(io.LimitReader(in, maxManifestBytes+1))
	reader.DisallowUnknownFields()
	if err = reader.Decode(value); err != nil {
		return domain.Fail("INVALID_ARGUMENT", "Invalid backup manifest")
	}
	if reader.Decode(new(any)) != io.EOF {
		return domain.Fail("INVALID_ARGUMENT", "Invalid backup manifest length")
	}
	return nil
}
func disjoint(a, b string) bool {
	a, err := canonical(a)
	if err != nil {
		return false
	}
	b, err = canonical(b)
	if err != nil {
		return false
	}
	for _, pair := range [][2]string{{a, b}, {b, a}} {
		rel, err := filepath.Rel(pair[0], pair[1])
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return false
		}
	}
	return true
}

func canonical(name string) (string, error) {
	name, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	var suffix []string
	for current := name; ; current = filepath.Dir(current) {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		}
		if !os.IsNotExist(err) || filepath.Dir(current) == current {
			return "", err
		}
		suffix = append(suffix, filepath.Base(current))
	}
}

func copyState(ctx context.Context, src, dst *os.Root, directory string, entries *[]File, total *int64) error {
	if _, err := src.Lstat(directory); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return fs.WalkDir(src.FS(), directory, func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return domain.Fail("ACCESS_DENIED", "Plugin state links are unsupported")
		}
		if entry.IsDir() {
			return nil
		}
		if len(*entries) >= MaxStateFiles {
			return domain.Fail("RATE_LIMITED", "Backup state file count exceeded")
		}
		in, err := regular(src, name)
		if err != nil {
			return err
		}
		defer in.Close()
		file, err := copyFile(ctx, in, dst, "state/"+name, min(maxFileBytes, MaxStateBytes-*total))
		if err != nil {
			return err
		}
		*total += file.Size
		*entries = append(*entries, file)
		return nil
	})
}

func syncTree(root *os.Root) error {
	var dirs []string
	err := fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			dirs = append(dirs, name)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		if err = syncDirectory(root, dirs[i]); err != nil {
			return err
		}
	}
	return nil
}
