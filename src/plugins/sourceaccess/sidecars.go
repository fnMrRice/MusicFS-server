package sourceaccess

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

// ScrapeFile 只在宿主内传递。成功项由调用方关闭，未能读取的旁车仅记录错误码。
type ScrapeFile struct {
	Reader      *StableReader
	SizeBytes   int64
	Version     string
	Kind, Match string
	Issue       string
}

type sidecarName struct{ name, kind, match string }

func sidecarNames(audioPath string, fields []string) []sidecarName {
	base := filepath.Base(filepath.FromSlash(audioPath))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	var names []sidecarName
	if slices.Contains(fields, "lyrics") {
		for _, ext := range []string{".lrc", ".txt"} {
			names = append(names, sidecarName{stem + ext, "lyrics", "exactStem"})
		}
	}
	if slices.Contains(fields, "artwork") {
		for _, ext := range []string{".jpg", ".jpeg", ".png"} {
			names = append(names, sidecarName{stem + ext, "artwork", "exactStem"})
		}
		for _, name := range []string{"cover.jpg", "cover.png", "folder.jpg", "folder.png"} {
			names = append(names, sidecarName{name, "artwork", "directoryCover"})
		}
	}
	return names
}

// OpenScrapeFiles 在同一任务下保护索引音频和固定名称的旁车。文件取得保护后才计算内容版本。
func (a *Access) OpenScrapeFiles(ctx context.Context, scope Scope, rootID, audioPath, version string, size int64, fields []string) ([]ScrapeFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if scope.UserID == "" || scope.InstanceID == "" || scope.TaskID == "" || scope.AssetID == "" || version == "" || size <= 0 || len(fields) < 1 || len(fields) > 2 {
		return nil, fmt.Errorf("ACCESS_DENIED: incomplete scraper scope")
	}
	for _, field := range fields {
		if field != "lyrics" && field != "artwork" {
			return nil, fmt.Errorf("ACCESS_DENIED: invalid sidecar field")
		}
	}
	root, err := a.openRoot(rootID)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	info, err := scrapeFileInfo(root, audioPath)
	if err != nil {
		return nil, err
	}
	if info.Size() != size {
		return nil, fmt.Errorf("RESOURCE_CHANGED: indexed audio size changed")
	}
	audio, err := a.protectScrapeFile(ctx, root, rootID, audioPath, info, version)
	if err != nil {
		return nil, err
	}
	files := []ScrapeFile{{Reader: audio, Version: version, SizeBytes: size, Kind: "audio", Match: "embedded"}}
	complete := false
	defer func() {
		if !complete {
			for _, file := range files {
				if file.Reader != nil {
					file.Reader.Close()
				}
			}
		}
	}()
	seen := map[string]bool{}
	var sidecarBytes int64
	for _, item := range sidecarNames(audioPath, fields) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if seen[item.name] {
			continue
		}
		seen[item.name] = true
		path := filepath.ToSlash(filepath.Join(filepath.Dir(filepath.FromSlash(audioPath)), item.name))
		info, err := scrapeFileInfo(root, path)
		if err != nil {
			continue
		}
		limit := int64(1 << 20)
		if item.kind == "artwork" {
			limit = 8 << 20
		}
		if info.Size() <= 0 || info.Size() > limit {
			continue
		}
		file := ScrapeFile{SizeBytes: info.Size(), Kind: item.kind, Match: item.match}
		if file.SizeBytes > (32<<20)-sidecarBytes {
			file.Issue = "UNSUPPORTED"
		} else {
			file.Reader, err = a.protectScrapeFile(ctx, root, rootID, path, info, "")
			if err != nil {
				file.Issue = pluginproto.ErrorCode(err)
			} else {
				file.Version = file.Reader.entry.ObservedVersion
				sidecarBytes += file.SizeBytes
			}
		}
		files = append(files, file)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := audio.Validate(); err != nil {
		return nil, err
	}
	complete = true
	return files, nil
}

func (a *Access) protectScrapeFile(ctx context.Context, root *os.Root, rootID, path string, expected os.FileInfo, version string) (*StableReader, error) {
	f, err := root.Open(filepath.FromSlash(path))
	if err != nil {
		return nil, fmt.Errorf("RESOURCE_CHANGED: scraper source could not be reopened")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !os.SameFile(expected, info) || !info.Mode().IsRegular() || info.Size() != expected.Size() {
		return nil, fmt.Errorf("RESOURCE_CHANGED: scraper source changed before protection")
	}
	identity, err := fileIdentity(f)
	if err != nil {
		return nil, fmt.Errorf("RESOURCE_CHANGED: scraper source identity is unavailable")
	}
	h := Handle{File: f, rootID: rootID, path: path, formats: a.formats,
		Entry:     pluginproto.Entry{RootID: rootID, RelativePath: path, FileIdentity: identity, SizeBytes: info.Size(), ObservedVersion: version},
		checkPath: func() error { return a.checkFileIdentity(rootID, path, identity) }}
	return h.Stable(ctx)
}

// 只准访问固定路径上的普通文件，并逐级排除链接。
func scrapeFileInfo(root *os.Root, path string) (os.FileInfo, error) {
	if !filepath.IsLocal(path) || path == "." {
		return nil, fmt.Errorf("ACCESS_DENIED: invalid scraper relative path")
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	prefix := ""
	var info os.FileInfo
	for _, part := range parts {
		prefix = filepath.Join(prefix, part)
		var err error
		info, err = root.Lstat(prefix)
		if err != nil {
			return nil, fmt.Errorf("RESOURCE_CHANGED: scraper path is unavailable")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("ACCESS_DENIED: linked scraper path refused")
		}
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("ACCESS_DENIED: scraper source is not a regular file")
	}
	return info, nil
}
