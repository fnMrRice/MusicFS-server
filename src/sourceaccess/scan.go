package sourceaccess

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func describe(f *os.File, rootID, path string) (pluginproto.Entry, error) {
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return pluginproto.Entry{}, fmt.Errorf("ACCESS_DENIED: source is not a regular file")
	}
	identity, err := fileIdentity(f)
	if err != nil {
		return pluginproto.Entry{}, fmt.Errorf("SOURCE_OFFLINE: file identity unavailable")
	}
	change, err := fileChangeVersion(f)
	if err != nil {
		return pluginproto.Entry{}, fmt.Errorf("SOURCE_OFFLINE: file change information unavailable")
	}
	links, err := fileLinkCount(f)
	if err != nil || links == 0 {
		return pluginproto.Entry{}, fmt.Errorf("SOURCE_OFFLINE: file link information unavailable")
	}
	version := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d:%s", identity, info.Size(), info.ModTime().UnixNano(), change)))
	entry := pluginproto.Entry{RootID: rootID, RelativePath: filepath.ToSlash(path), FileIdentity: identity, LinkCount: links, ObservedVersion: fmt.Sprintf("%x", version), SizeBytes: info.Size(), MIME: "application/octet-stream"}
	var header [64]byte
	n, _ := f.ReadAt(header[:], 0)
	switch {
	case n >= 8 && string(header[:8]) == "CTENFDAM":
		entry.MIME = "audio/x-ncm"
	case n >= 44 && string(header[:4]) == "RIFF" && string(header[8:12]) == "WAVE":
		entry.MIME = "audio/wav"
		if string(header[12:16]) == "fmt " && binary.LittleEndian.Uint32(header[16:20]) == 16 && string(header[36:40]) == "data" {
			rate := binary.LittleEndian.Uint32(header[28:32])
			size := binary.LittleEndian.Uint32(header[40:44])
			if rate > 0 && int64(size) <= info.Size()-44 {
				d := int64(size) * 1000 / int64(rate)
				entry.DurationMs = &d
			}
		}
	case n >= 4 && string(header[:4]) == "fLaC":
		entry.MIME = "audio/flac"
	case n >= 3 && string(header[:3]) == "ID3", n >= 2 && header[0] == 0xff && header[1]&0xe0 == 0xe0:
		entry.MIME = "audio/mpeg"
	case n >= 4 && string(header[:4]) == "OggS":
		entry.MIME = "audio/ogg"
	case n >= 8 && string(header[4:8]) == "ftyp":
		entry.MIME = "audio/mp4"
	}
	return entry, nil
}

func (a *Access) Entries(ctx context.Context, scope pluginproto.ScanScope) ([]pluginproto.Entry, string, []string, error) {
	if err := scope.Validate(); err != nil {
		return nil, "", nil, err
	}
	rootID := scope.RootID
	if root, ok := a.roots[rootID]; !ok || root.ExpectedIdentity != scope.RootIdentity {
		return nil, "", nil, fmt.Errorf("ACCESS_DENIED: scan root is not authorized")
	}
	r, err := a.openRoot(rootID)
	if err != nil {
		return nil, "", nil, err
	}
	defer r.Close()
	entries := make([]pluginproto.Entry, 0)
	issues := make([]string, 0)
	err = fs.WalkDir(r.FS(), ".", func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if len(issues) < 100 {
				issues = append(issues, "unreadable branch")
			}
			return nil
		}
		if path != "." && pluginproto.Excludes(scope.ExcludePatterns, path) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			if len(issues) < 100 {
				issues = append(issues, "symbolic link skipped")
			}
			return nil
		}
		if d.IsDir() {
			if !scope.ContainsDirectory(path) {
				return fs.SkipDir
			}
			return nil
		}
		if !scope.Contains(rootID, path) {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !strings.Contains("|.wav|.mp3|.flac|.ogg|.m4a|.aac|.aiff|.aif|.opus|.ncm|", "|"+ext+"|") {
			return nil
		}
		if len(entries) >= 50000 {
			return fmt.Errorf("RATE_LIMITED: scan entry limit reached")
		}
		f, err := r.Open(path)
		if err != nil {
			if len(issues) < 100 {
				issues = append(issues, "unreadable file")
			}
			return nil
		}
		entry, err := describe(f, rootID, path)
		f.Close()
		if err != nil {
			if len(issues) < 100 {
				issues = append(issues, "invalid file")
			}
			return nil
		}
		entries = append(entries, entry)
		return nil
	})
	if err != nil && err != io.EOF {
		return nil, "", issues, err
	}
	if _, err = a.openRootCheck(rootID); err != nil {
		return nil, "", issues, err
	}
	return entries, a.roots[rootID].ExpectedIdentity, issues, nil
}

func (a *Access) openRootCheck(rootID string) (bool, error) {
	r, err := a.openRoot(rootID)
	if err != nil {
		return false, err
	}
	r.Close()
	return true, nil
}
