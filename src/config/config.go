package config

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type MusicRoot struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

type Config struct {
	Listen            string      `json:"listen"`
	DataRoot          string      `json:"dataRoot"`
	MediaRoot         string      `json:"mediaRoot"`
	CacheRoot         string      `json:"cacheRoot"`
	PluginPath        string      `json:"pluginPath"`
	AudioWorkerPath   string      `json:"audioWorkerPath"`
	NCMWorkerPath     string      `json:"ncmWorkerPath"`
	NCMWorkerSHA256   string      `json:"ncmWorkerSha256"`
	PluginSHA256      string      `json:"pluginSha256"`
	AudioWorkerSHA256 string      `json:"audioWorkerSha256"`
	FFmpegPath        string      `json:"ffmpegPath"`
	FFprobePath       string      `json:"ffprobePath"`
	WebRoot           string      `json:"webRoot"`
	AdminToken        string      `json:"adminToken"`
	AllowedOrigins    []string    `json:"allowedOrigins"`
	AllowLoopbackHTTP bool        `json:"allowLoopbackHttp"`
	TLSCert           string      `json:"tlsCert"`
	TLSKey            string      `json:"tlsKey"`
	MusicRoots        []MusicRoot `json:"musicRoots"`
	MaxSessions       int         `json:"maxSessions"`
	MaxProcessing     int         `json:"maxProcessing"`
	MaxCacheBytes     int64       `json:"maxCacheBytes"`
	SessionTTLSeconds int         `json:"sessionTtlSeconds"`
}

func Load(path string, overrides map[string]string) (Config, error) {
	c := Config{Listen: "127.0.0.1:8080", DataRoot: "data", MediaRoot: "media", CacheRoot: "cache", MaxSessions: 8, MaxProcessing: 2, MaxCacheBytes: 1 << 30, SessionTTLSeconds: 3600}
	base, _ := os.Getwd()
	if path != "" {
		f, err := os.Open(path)
		if err != nil {
			return c, errors.New("cannot read configuration file")
		}
		defer f.Close()
		d := json.NewDecoder(io.LimitReader(f, 1<<20))
		d.DisallowUnknownFields()
		if err := d.Decode(&c); err != nil {
			return c, errors.New("invalid configuration JSON or unknown field")
		}
		if err := d.Decode(new(any)); err != io.EOF {
			return c, errors.New("configuration must contain one JSON object")
		}
		abs, _ := filepath.Abs(path)
		base = filepath.Dir(abs)
	}
	fields := map[string]*string{"LISTEN": &c.Listen, "DATA_ROOT": &c.DataRoot, "MEDIA_ROOT": &c.MediaRoot, "CACHE_ROOT": &c.CacheRoot, "ADMIN_TOKEN": &c.AdminToken, "PLUGIN_PATH": &c.PluginPath, "FFMPEG_PATH": &c.FFmpegPath, "FFPROBE_PATH": &c.FFprobePath, "WEB_ROOT": &c.WebRoot}
	for key, target := range fields {
		if value, ok := os.LookupEnv("MUSICFS_" + key); ok {
			*target = value
		}
		if value, ok := overrides[key]; ok {
			*target = value
		}
	}
	for _, field := range []*string{&c.DataRoot, &c.MediaRoot, &c.CacheRoot, &c.WebRoot, &c.PluginPath, &c.AudioWorkerPath, &c.NCMWorkerPath, &c.FFmpegPath, &c.FFprobePath, &c.TLSCert, &c.TLSKey} {
		if *field != "" && !filepath.IsAbs(*field) {
			*field = filepath.Join(base, *field)
		}
	}
	for i := range c.MusicRoots {
		if c.MusicRoots[i].Path == "" {
			return c, errors.New("musicRoots path must not be empty")
		}
		if !filepath.IsAbs(c.MusicRoots[i].Path) {
			c.MusicRoots[i].Path = filepath.Join(base, c.MusicRoots[i].Path)
		}
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	host, _, err := net.SplitHostPort(c.Listen)
	if err != nil {
		return errors.New("invalid listen address")
	}
	ip := net.ParseIP(host)
	loopback := ip != nil && ip.IsLoopback()
	if len(c.AdminToken) < 24 {
		return errors.New("adminToken must have at least 24 characters")
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return errors.New("tlsCert and tlsKey must be set together")
	}
	if c.TLSCert == "" && (!loopback || !c.AllowLoopbackHTTP) {
		return errors.New("HTTP requires explicit loopback-only configuration; otherwise configure TLS")
	}
	if c.MaxSessions < 1 || c.MaxSessions > 128 || c.MaxProcessing < 1 || c.MaxProcessing > 8 || c.MaxCacheBytes < 1<<20 || c.SessionTTLSeconds < 60 || c.SessionTTLSeconds > 86400 {
		return errors.New("invalid resource limits")
	}
	for _, hash := range []string{c.PluginSHA256, c.AudioWorkerSHA256, c.NCMWorkerSHA256} {
		if hash != "" {
			raw, err := hex.DecodeString(hash)
			if err != nil || len(raw) != 32 {
				return errors.New("plugin checksums must be SHA-256 hex strings")
			}
		}
	}
	if len(c.AllowedOrigins) == 0 {
		return errors.New("allowedOrigins must be explicit")
	}
	for _, origin := range c.AllowedOrigins {
		u, e := url.Parse(origin)
		if e != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return errors.New("invalid allowedOrigins entry")
		}
		if u.Scheme != "https" && !(u.Scheme == "http" && c.AllowLoopbackHTTP && net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback()) {
			return errors.New("allowedOrigins requires HTTPS or explicit loopback HTTP")
		}
	}
	writable := []string{c.DataRoot, c.MediaRoot, c.CacheRoot}
	for i, p := range writable {
		if p == "" {
			return errors.New("writable roots must not be empty")
		}
		for j, q := range writable {
			if i != j && overlaps(p, q) {
				return errors.New("data, media and cache roots must be separate")
			}
		}
	}
	ids := map[string]bool{}
	for _, root := range c.MusicRoots {
		if root.ID == "" || strings.ContainsAny(root.ID, "/\\.:") || root.Name == "" || root.Path == "" || ids[root.ID] {
			return errors.New("musicRoots requires unique IDs and nonempty names/paths")
		}
		ids[root.ID] = true
		for _, p := range writable {
			if overlaps(p, root.Path) {
				return fmt.Errorf("music root %s overlaps writable storage", root.ID)
			}
		}
	}
	return nil
}

func overlaps(a, b string) bool {
	a = canonicalPath(a)
	b = canonicalPath(b)
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
