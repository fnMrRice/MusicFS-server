package config

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

func resolvePaths(c *Config, base string) {
	resolve := func(value *string) {
		if *value != "" && !filepath.IsAbs(*value) {
			*value = filepath.Join(base, *value)
		}
	}
	resolve(&c.PluginBundle)
	resolve(&c.Storage.Executable)
	if c.Storage.Backend == "postgres" {
		for _, key := range []string{"passwordFile", "sslRootCert"} {
			if path := c.Storage.Options[key]; path != "" {
				resolve(&path)
				c.Storage.Options[key] = path
			}
		}
	}
	c.Storage.SHA256 = strings.ToLower(c.Storage.SHA256)
	for i := range c.AudioDSP.Plugins {
		resolve(&c.AudioDSP.Plugins[i].Executable)
		c.AudioDSP.Plugins[i].SHA256 = strings.ToLower(c.AudioDSP.Plugins[i].SHA256)
	}
	for _, field := range []*string{&c.DataRoot, &c.MediaRoot, &c.CacheRoot, &c.WebRoot, &c.TLSCert, &c.TLSKey, &c.Fingerprinting.FpcalcPath} {
		resolve(field)
	}
	if c.MediaRoot == "" {
		c.MediaRoot = filepath.Join(c.DataRoot, "media")
	}
	if c.Logging.Directory == "" {
		c.Logging.Directory = filepath.Join(c.DataRoot, "logs")
	} else {
		resolve(&c.Logging.Directory)
	}
	c.Fingerprinting.FpcalcSHA256 = strings.ToLower(c.Fingerprinting.FpcalcSHA256)
	for i := range c.Plugins {
		plugin := &c.Plugins[i]
		plugin.SHA256 = strings.ToLower(plugin.SHA256)
		resolve(&plugin.Executable)
		for j := range plugin.Instances {
			instance := &plugin.Instances[j]
			if instance.Permissions != nil {
				for k := range instance.Permissions.ReadDirectories {
					resolve(&instance.Permissions.ReadDirectories[k])
				}
			}
			resolve(&instance.Config.FFmpegPath)
			resolve(&instance.Config.FFprobePath)
			for k := range instance.Config.MusicRoots {
				resolve(&instance.Config.MusicRoots[k].Path)
			}
		}
	}
}

// 在线下载由播放授权保护，单独清理。其专属目录不能对外静态发布或混入插件状态备份。
func (c Config) ValidateDownloadDirectory(dir string) error {
	if c.WebRoot != "" && overlaps(c.WebRoot, dir) {
		return domain.Fail("INVALID_ARGUMENT", "The download directory overlaps the public web directory")
	}
	for _, state := range []string{"providers", "online", "writeback"} {
		if c.DataRoot != "" && overlaps(filepath.Join(c.DataRoot, state), dir) {
			return domain.Fail("INVALID_ARGUMENT", "The download directory overlaps a plugin state directory")
		}
	}
	return nil
}

func overlaps(a, b string) bool {
	a = canonicalPath(a)
	b = canonicalPath(b)
	if runtime.GOOS == "windows" {
		a, b = strings.ToLower(a), strings.ToLower(b)
	}
	for _, pair := range [][2]string{{a, b}, {b, a}} {
		r, err := filepath.Rel(pair[0], pair[1])
		if err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// 媒体目录可位于部署数据根内，但不能与根本身相同或通过链接逃到根的上层。
func mediaInsideData(data, media string) bool {
	data, media = canonicalPath(data), canonicalPath(media)
	if runtime.GOOS == "windows" {
		data, media = strings.ToLower(data), strings.ToLower(media)
	}
	relative, err := filepath.Rel(data, media)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func canonicalPath(path string) string {
	path = filepath.Clean(path)
	var suffix []string
	for current := path; ; current = filepath.Dir(current) {
		if resolved, err := filepath.EvalSymlinks(current); err == nil {
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved
		}
		if filepath.Dir(current) == current {
			return path
		}
		suffix = append(suffix, filepath.Base(current))
	}
}
