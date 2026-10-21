// Package mediastore stores immutable content separately from database indexes.
package mediastore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

const MaxObjectBytes int64 = 16 << 20

var objectID = regexp.MustCompile(`^[0-9a-f]{48}$`)

type Object struct {
	ID           string `json:"objectId"`
	Kind         string `json:"kind"`
	MIME         string `json:"mimeType"`
	Size         int64  `json:"sizeBytes"`
	SHA256       string `json:"sha256"`
	RelativePath string `json:"-"`
}

func (o Object) Validate() error {
	b, err := hex.DecodeString(o.SHA256)
	if !objectID.MatchString(o.ID) || o.Kind == "" || len(o.Kind) > 64 || o.MIME == "" || len(o.MIME) > 128 || o.Size < 1 || o.Size > MaxObjectBytes || err != nil || len(b) != 32 || o.SHA256 != strings.ToLower(o.SHA256) || o.RelativePath != "objects/"+o.ID {
		return domain.Fail("INVALID_ARGUMENT", "Invalid media object")
	}
	return nil
}

type Files struct{ root *os.Root }
type Validator func(io.ReadSeeker) error

func OpenFiles(path string) (*Files, error) {
	if !filepath.IsAbs(path) {
		return nil, domain.Fail("INVALID_ARGUMENT", "Media root must be absolute")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	f := &Files{root: r}
	for _, name := range []string{"staging", "objects"} {
		if err := r.Mkdir(name, 0700); err != nil && !os.IsExist(err) {
			r.Close()
			return nil, err
		}
		info, err := r.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			r.Close()
			return nil, domain.Fail("ACCESS_DENIED", "Media directories must be regular directories")
		}
	}
	return f, nil
}

func (f *Files) Close() error { return f.root.Close() }

// Write creates an exclusive staging file. Its durable database publication
// record must already exist. A failed write never publishes an object.
func (f *Files) Write(ctx context.Context, id, kind, mime string, input io.Reader, limit int64, validate Validator) (Object, error) {
	if !objectID.MatchString(id) || limit < 1 || limit > MaxObjectBytes || validate == nil {
		return Object{}, domain.Fail("INVALID_ARGUMENT", "Invalid media write")
	}
	path := "staging/" + id
	out, err := f.root.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return Object{}, err
	}
	keep := false
	defer func() {
		out.Close()
		if !keep {
			f.root.Remove(path)
		}
	}()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, hash), io.LimitReader(contextReader{ctx, input}, limit+1))
	if err != nil {
		return Object{}, err
	}
	if n == 0 || n > limit {
		return Object{}, domain.Fail("INVALID_ARGUMENT", "Media object size exceeds its allowed range")
	}
	if _, err = out.Seek(0, io.SeekStart); err != nil {
		return Object{}, err
	}
	if err = validate(out); err != nil {
		return Object{}, err
	}
	if err = ctx.Err(); err != nil {
		return Object{}, err
	}
	o := Object{ID: id, Kind: kind, MIME: mime, Size: n, SHA256: hex.EncodeToString(hash.Sum(nil)), RelativePath: "objects/" + id}
	if err = o.Validate(); err != nil {
		return Object{}, err
	}
	if err = out.Sync(); err != nil {
		return Object{}, err
	}
	if err = out.Close(); err != nil {
		return Object{}, err
	}
	if err = syncDirectory(f.root, "staging"); err != nil {
		return Object{}, err
	}
	keep = true
	return o, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func (f *Files) verified(o Object, path string) (*os.File, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	info, err := f.root.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, domain.Fail("RESOURCE_CHANGED", "Media content is missing or invalid")
	}
	in, err := f.root.Open(path)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*os.File, error) { in.Close(); return nil, err }
	stat, err := in.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() != o.Size {
		return fail(domain.Fail("RESOURCE_CHANGED", "Media content size changed"))
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(in, o.Size+1))
	if err != nil || n != o.Size || hex.EncodeToString(hash.Sum(nil)) != o.SHA256 {
		return fail(domain.Fail("RESOURCE_CHANGED", "Media content checksum changed"))
	}
	if _, err = in.Seek(0, io.SeekStart); err != nil {
		return fail(err)
	}
	return in, nil
}

// Publish is replayable after a crash between rename and journal advancement.
// An existing destination must match the recorded bytes; it is never replaced.
func (f *Files) Publish(o Object) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if _, err := f.root.Lstat(o.RelativePath); err == nil {
		in, err := f.verified(o, o.RelativePath)
		if err != nil {
			return err
		}
		if err = in.Close(); err != nil {
			return err
		}
		if err = syncDirectory(f.root, "objects"); err != nil {
			return err
		}
		if err = f.RemoveStaging(o.ID); err != nil {
			return err
		}
		return syncDirectory(f.root, "staging")
	} else if !os.IsNotExist(err) {
		return err
	}
	stage := "staging/" + o.ID
	in, err := f.verified(o, stage)
	if err != nil {
		return err
	}
	if err = in.Close(); err != nil {
		return err
	}
	if err = publishFile(f.root, stage, o.RelativePath); err != nil {
		return fmt.Errorf("cannot publish media object: %w", err)
	}
	return nil
}

// Open verifies the immutable bytes before returning an internal file handle.
// The caller must authorize the object through a live database reference first.
func (f *Files) Open(o Object) (*os.File, error) { return f.verified(o, o.RelativePath) }

func (f *Files) RemoveStaging(id string) error {
	if !objectID.MatchString(id) {
		return domain.Fail("INVALID_ARGUMENT", "Invalid media object ID")
	}
	err := f.root.Remove("staging/" + id)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// RemoveObject is only for the reference-aware collector after its durable claim.
func (f *Files) RemoveObject(o Object) error {
	if err := o.Validate(); err != nil {
		return err
	}
	err := f.root.Remove(o.RelativePath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return syncDirectory(f.root, "objects")
}
