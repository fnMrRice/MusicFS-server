package config

import (
	"path/filepath"
	"runtime"
	"strings"
)

func resolvePaths(c *Config, base string) {
	resolve := func(value *string) {
		if *value != "" && !filepath.IsAbs(*value) {
			*value = filepath.Join(base, *value)
		}
	}
	resolve(&c.PluginBundle)
	for _, field := range []*string{&c.DataRoot, &c.MediaRoot, &c.CacheRoot, &c.WebRoot, &c.TLSCert, &c.TLSKey, &c.Fingerprinting.FpcalcPath} {
		resolve(field)
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
			if instance.Name == "" {
				instance.Name = instance.InstanceID
			}
			resolve(&instance.Config.FFmpegPath)
			resolve(&instance.Config.FFprobePath)
			for k := range instance.Config.MusicRoots {
				resolve(&instance.Config.MusicRoots[k].Path)
			}
		}
	}
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
