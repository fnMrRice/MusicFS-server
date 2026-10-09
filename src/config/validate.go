package config

import (
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
)

func (c Config) Validate() error {
	if c.ConfigVersion != 1 {
		return errors.New("configVersion must be 1")
	}
	host, port, err := net.SplitHostPort(c.Listen)
	number, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || number < 1 || number > 65535 {
		return errors.New("invalid listen address")
	}
	ip := net.ParseIP(host)
	loopback := ip != nil && ip.IsLoopback()
	if len(c.AdminToken) < 24 {
		return errors.New("administrator credential must have at least 24 characters")
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return errors.New("tlsCert and tlsKey must be set together")
	}
	if strings.ContainsRune(c.WebRoot, 0) || strings.ContainsRune(c.TLSCert, 0) || strings.ContainsRune(c.TLSKey, 0) {
		return errors.New("invalid webRoot or TLS path")
	}
	if c.TLSCert == "" && (!loopback || !c.AllowLoopbackHTTP) {
		return errors.New("HTTP requires explicit loopback-only configuration; otherwise configure TLS")
	}
	if c.MaxSessions < 1 || c.MaxSessions > 128 || c.MaxProcessing < 1 || c.MaxProcessing > 8 || c.MaxCacheBytes < 1<<20 || c.SessionTTLSeconds < 60 || c.SessionTTLSeconds > 86400 {
		return errors.New("invalid resource limits")
	}
	if len(c.AllowedOrigins) == 0 {
		return errors.New("allowedOrigins must be explicit")
	}
	for _, origin := range c.AllowedOrigins {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
			return errors.New("invalid allowedOrigins entry")
		}
		if u.Port() != "" {
			port, err := strconv.Atoi(u.Port())
			if err != nil || port < 1 || port > 65535 {
				return errors.New("invalid allowedOrigins port")
			}
		}
		if u.Scheme != "https" && !(u.Scheme == "http" && c.AllowLoopbackHTTP && net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback()) {
			return errors.New("allowedOrigins requires HTTPS or explicit loopback HTTP")
		}
	}
	writable := []string{c.DataRoot, c.MediaRoot, c.CacheRoot}
	for i, path := range writable {
		if path == "" || strings.ContainsRune(path, 0) {
			return errors.New("writable roots must be nonempty paths")
		}
		for j := i + 1; j < len(writable); j++ {
			if overlaps(path, writable[j]) {
				return errors.New("data, media and cache roots must be separate")
			}
		}
	}
	return c.validatePlugins(writable)
}

func (c Config) validatePlugins(writable []string) error {
	plugins, instances := map[string]bool{}, map[string]bool{}
	for _, plugin := range c.Plugins {
		role := "audio"
		switch plugin.PluginID {
		case "musicfs.localfiles":
			role = "provider"
		case "musicfs.audio.ffmpeg", "musicfs.ncm":
		default:
			return errors.New("unsupported pluginId")
		}
		if plugins[plugin.PluginID] {
			return errors.New("pluginId must be unique")
		}
		plugins[plugin.PluginID] = true
		if plugin.SHA256 != "" {
			hash, err := hex.DecodeString(plugin.SHA256)
			if err != nil || len(hash) != 32 {
				return errors.New("plugin sha256 must contain 64 hexadecimal characters")
			}
		}
		if strings.ContainsRune(plugin.Executable, 0) {
			return errors.New("invalid plugin executable path")
		}
		active := 0
		for _, instance := range plugin.Instances {
			identityKey := strings.ToLower(instance.InstanceID)
			if !validConfigID(instance.InstanceID) || instances[identityKey] {
				return errors.New("instanceId must be valid and globally unique ignoring case")
			}
			instances[identityKey] = true
			if instance.Role != role {
				return errors.New("instance role does not match pluginId")
			}
			enabled := plugin.Enabled && instance.Enabled
			if enabled {
				active++
				if plugin.Executable == "" {
					return errors.New("enabled plugin instance requires an executable path")
				}
			}
			if err := validateSettings(plugin.PluginID, instance.Config, enabled, writable); err != nil {
				return err
			}
		}
		if role == "audio" && active > 1 {
			return errors.New("audio plugins support at most one effectively enabled instance each")
		}
	}
	return nil
}

func validConfigID(id string) bool {
	if len(id) < 1 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

func validateSettings(plugin string, settings InstanceConfig, enabled bool, writable []string) error {
	if strings.ContainsRune(settings.FFmpegPath, 0) || strings.ContainsRune(settings.FFprobePath, 0) {
		return errors.New("invalid audio tool path")
	}
	if plugin != "musicfs.localfiles" && len(settings.MusicRoots) != 0 || plugin != "musicfs.audio.ffmpeg" && (settings.FFmpegPath != "" || settings.FFprobePath != "") {
		return errors.New("instance config does not match pluginId")
	}
	if enabled && plugin == "musicfs.audio.ffmpeg" && (settings.FFmpegPath == "" || settings.FFprobePath == "") {
		return errors.New("enabled audio instance requires ffmpegPath and ffprobePath")
	}
	if enabled && plugin == "musicfs.localfiles" && len(settings.MusicRoots) == 0 {
		return errors.New("enabled local files instance requires musicRoots")
	}
	ids := map[string]bool{}
	for _, root := range settings.MusicRoots {
		if !validConfigID(root.ID) || root.Name == "" || root.Path == "" || strings.ContainsRune(root.Path, 0) || ids[root.ID] {
			return errors.New("musicRoots requires valid unique IDs and nonempty names and paths")
		}
		ids[root.ID] = true
		for _, path := range writable {
			if overlaps(path, root.Path) {
				return errors.New("music root overlaps writable storage")
			}
		}
	}
	return nil
}
