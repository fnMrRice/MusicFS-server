package config

import (
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

type MusicRoot struct {
	ID   string `yaml:"id" json:"id"`
	Name string `yaml:"name" json:"name"`
	Path string `yaml:"path" json:"path"`
}

type Plugin struct {
	PluginID   string     `yaml:"pluginId" json:"pluginId"`
	Executable string     `yaml:"executable,omitempty" json:"executable,omitempty"`
	Args       []string   `yaml:"args,omitempty" json:"args,omitempty"`
	SHA256     string     `yaml:"sha256,omitempty" json:"sha256,omitempty"`
	Enabled    bool       `yaml:"enabled" json:"enabled"`
	Instances  []Instance `yaml:"instances" json:"instances"`
}

type Instance struct {
	Permissions          *Permissions                 `yaml:"permissions,omitempty" json:"permissions,omitempty"`
	PendingConfiguration bool                         `yaml:"-" json:"-"`
	InstanceID           string                       `yaml:"instanceId" json:"instanceId"`
	Name                 string                       `yaml:"name,omitempty" json:"name,omitempty"`
	Role                 string                       `yaml:"role" json:"role"`
	Enabled              bool                         `yaml:"enabled" json:"enabled"`
	Config               InstanceConfig               `yaml:"config" json:"config"`
	ImportDefaults       *pluginproto.ImportOverrides `yaml:"importDefaults,omitempty" json:"importDefaults,omitempty"`
}

type Permissions struct {
	ReadDirectories []string `yaml:"readDirectories" json:"readDirectories"`
}

type InstanceConfig struct {
	Watch       *WatchConfig `yaml:"watch,omitempty" json:"watch,omitempty"`
	MusicRoots  []MusicRoot  `yaml:"musicRoots,omitempty" json:"musicRoots,omitempty"`
	FFmpegPath  string       `yaml:"ffmpegPath,omitempty" json:"ffmpegPath,omitempty"`
	FFprobePath string       `yaml:"ffprobePath,omitempty" json:"ffprobePath,omitempty"`
}

type HashingConfig struct {
	Auto           bool  `yaml:"auto" json:"auto"`
	MaxConcurrent  int   `yaml:"maxConcurrent" json:"maxConcurrent"`
	BytesPerSecond int64 `yaml:"bytesPerSecond" json:"bytesPerSecond"`
}

type FingerprintingConfig struct {
	Enabled        bool   `yaml:"enabled" json:"enabled"`
	FpcalcPath     string `yaml:"fpcalcPath" json:"fpcalcPath"`
	FpcalcSHA256   string `yaml:"fpcalcSha256" json:"fpcalcSha256"`
	MaxConcurrent  int    `yaml:"maxConcurrent" json:"maxConcurrent"`
	TimeoutSeconds int    `yaml:"timeoutSeconds" json:"timeoutSeconds"`
}

type MetadataConfig struct {
	Automatic enrichment.AutomaticPolicy `yaml:"automatic" json:"automatic"`
}

type Config struct {
	PluginBundle      string               `yaml:"pluginBundle,omitempty" json:"pluginBundle,omitempty"`
	SourcePath        string               `yaml:"-" json:"-"`
	SourceOverrides   map[string]string    `yaml:"-" json:"-"`
	ConfigVersion     int                  `yaml:"configVersion" json:"configVersion"`
	Listen            string               `yaml:"listen" json:"listen"`
	DataRoot          string               `yaml:"dataRoot" json:"dataRoot"`
	MediaRoot         string               `yaml:"mediaRoot" json:"mediaRoot"`
	CacheRoot         string               `yaml:"cacheRoot" json:"cacheRoot"`
	WebRoot           string               `yaml:"webRoot,omitempty" json:"webRoot,omitempty"`
	AdminToken        string               `yaml:"-" json:"-"`
	AllowedOrigins    []string             `yaml:"allowedOrigins" json:"allowedOrigins"`
	AllowLoopbackHTTP bool                 `yaml:"allowLoopbackHttp" json:"allowLoopbackHttp"`
	TLSCert           string               `yaml:"tlsCert,omitempty" json:"tlsCert,omitempty"`
	TLSKey            string               `yaml:"tlsKey,omitempty" json:"tlsKey,omitempty"`
	Plugins           []Plugin             `yaml:"plugins" json:"plugins"`
	MaxSessions       int                  `yaml:"maxSessions" json:"maxSessions"`
	MaxProcessing     int                  `yaml:"maxProcessing" json:"maxProcessing"`
	MaxCacheBytes     int64                `yaml:"maxCacheBytes" json:"maxCacheBytes"`
	SessionTTLSeconds int                  `yaml:"sessionTtlSeconds" json:"sessionTtlSeconds"`
	Hashing           HashingConfig        `yaml:"hashing" json:"hashing"`
	Fingerprinting    FingerprintingConfig `yaml:"fingerprinting,omitempty" json:"fingerprinting"`
	Metadata          MetadataConfig       `yaml:"metadata" json:"metadata"`
	WriteBack         WriteBackConfig      `yaml:"writeBack,omitempty" json:"writeBack"`
}

func defaults() Config {
	return Config{Listen: "127.0.0.1:8080", DataRoot: "data", MediaRoot: "media", CacheRoot: "cache", MaxSessions: 8, MaxProcessing: 2, MaxCacheBytes: 1 << 30, SessionTTLSeconds: 3600,
		Hashing:        HashingConfig{MaxConcurrent: 1, BytesPerSecond: 4 << 20},
		Fingerprinting: FingerprintingConfig{MaxConcurrent: 1, TimeoutSeconds: 30},
		Metadata:       MetadataConfig{Automatic: enrichment.AutomaticPolicy{Fields: []string{"lyrics", "artwork"}}},
		WriteBack:      WriteBackConfig{MaxFileBytes: 8 << 30, Roots: []WriteBackRoot{}}}
}
