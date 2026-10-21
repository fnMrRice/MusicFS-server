package config

import (
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/pluginconfig"
)

func (c Config) Validate() error {
	if err := c.Storage.Validate(); err != nil {
		return err
	}
	if err := c.AudioDSP.Validate(); err != nil {
		return err
	}
	if len(c.AudioDSP.Chain) > 0 {
		ready := false
		for _, plugin := range c.Plugins {
			if plugin.PluginID == "musicfs.plugin.decoder.ffmpeg" {
				for _, instance := range plugin.Instances {
					ready = ready || plugin.CanStart(instance)
				}
			}
		}
		if !ready {
			return errors.New("configured DSP chain requires an enabled audio instance")
		}
	}
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
	if strings.ContainsRune(c.PluginBundle, 0) || strings.ContainsRune(c.WebRoot, 0) || strings.ContainsRune(c.TLSCert, 0) || strings.ContainsRune(c.TLSKey, 0) {
		return errors.New("invalid webRoot or TLS path")
	}
	if c.TLSCert == "" && (!loopback || !c.AllowLoopbackHTTP) {
		return errors.New("HTTP requires explicit loopback-only configuration; otherwise configure TLS")
	}
	if c.MaxSessions < 1 || c.MaxSessions > 128 || c.MaxProcessing < 1 || c.MaxProcessing > 8 || c.MaxCacheBytes < 1<<20 || c.SessionTTLSeconds < 60 || c.SessionTTLSeconds > 86400 {
		return errors.New("invalid resource limits")
	}
	if c.Hashing.MaxConcurrent < 1 || c.Hashing.MaxConcurrent > 4 || c.Hashing.BytesPerSecond < 1<<16 || c.Hashing.BytesPerSecond > 1<<30 {
		return errors.New("invalid hashing resource limits")
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
	if err := c.validatePlugins(writable); err != nil {
		return err
	}
	if err := c.validateFingerprinting(); err != nil {
		return err
	}
	if err := c.validateMetadata(); err != nil {
		return err
	}
	return c.validateWriteBack()
}

func (c Config) validatePlugins(writable []string) error {
	plugins, instances := map[string]bool{}, map[string]bool{}
	onlineInstances := 0
	watchedInstances := 0
	for _, plugin := range c.Plugins {
		role := pluginconfig.Role(plugin.PluginID)
		if role == "" {
			return errors.New("unsupported pluginId")
		}
		if plugins[plugin.PluginID] {
			return errors.New("pluginId must be unique")
		}
		if !pluginconfig.SupportsMultipleInstances(plugin.PluginID) && len(plugin.Instances) > 1 {
			return errors.New("plugin supports at most one configured instance")
		}
		if role == "scraper" && len(plugin.Instances) > 16 {
			return errors.New("local scraper supports at most 16 configured instances")
		}
		plugins[plugin.PluginID] = true
		if OnlinePlatform(plugin.PluginID) != "" {
			onlineInstances += len(plugin.Instances)
			if onlineInstances > 16 {
				return errors.New("at most 16 online instances are supported")
			}
		}
		if plugin.SHA256 != "" {
			hash, err := hex.DecodeString(plugin.SHA256)
			if err != nil || len(hash) != 32 {
				return errors.New("plugin sha256 must contain 64 hexadecimal characters")
			}
		}
		if strings.ContainsRune(plugin.Executable, 0) {
			return errors.New("invalid plugin executable path")
		}
		if len(plugin.Args) > 64 {
			return errors.New("plugin args cannot exceed 64 entries")
		}
		for _, arg := range plugin.Args {
			if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
				return errors.New("invalid plugin argument")
			}
		}
		for _, instance := range plugin.Instances {
			if instance.OwnerUserID != "" && (OnlinePlatform(plugin.PluginID) == "" || !pluginproto.OnlineIdentity(instance.OwnerUserID)) {
				return errors.New("ownerUserId requires an online plugin and a valid user identity")
			}
			if instance.Permissions != nil {
				if plugin.PluginID != "musicfs.plugin.source.localfiles" || len(instance.Permissions.ReadDirectories) > 256 {
					return errors.New("invalid filesystem permissions")
				}
				for _, path := range instance.Permissions.ReadDirectories {
					if path == "" || strings.ContainsRune(path, 0) {
						return errors.New("invalid allowed directory")
					}
				}
			}
			if _, err := EffectiveImportDefaults(plugin.PluginID, instance); err != nil {
				return err
			}
			for _, root := range instance.Config.MusicRoots {
				if _, err := EffectiveRootImportDefaults(instance, root); err != nil {
					return err
				}
			}
			identityKey := strings.ToLower(instance.InstanceID)
			if !instanceid.Valid(instance.InstanceID) || instances[identityKey] {
				return errors.New("instanceId must be a UUID and globally unique ignoring case")
			}
			instances[identityKey] = true
			enabled := plugin.CanStart(instance)
			if enabled && instance.Config.Watch != nil && instance.Config.Watch.Enabled {
				watchedInstances++
				if watchedInstances > MaxWatchedInstances {
					return errors.New("at most 8 enabled watch instances are supported")
				}
			}
			if enabled {
				if plugin.Executable == "" {
					return errors.New("enabled plugin instance requires an executable path")
				}
			}
			if err := validateSettings(plugin.PluginID, instance.Config, enabled, writable); err != nil {
				return err
			}
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
	if settings.Watch != nil {
		if plugin != "musicfs.plugin.source.localfiles" {
			return errors.New("watch is available only for local files")
		}
		if err := settings.Watch.Validate(); err != nil {
			return err
		}
	}
	if len(settings.MusicRoots) > 256 {
		return errors.New("musicRoots cannot exceed 256 authorized roots per instance")
	}
	if strings.ContainsRune(settings.FFmpegPath, 0) || strings.ContainsRune(settings.FFprobePath, 0) {
		return errors.New("invalid audio tool path")
	}
	if plugin != "musicfs.plugin.source.localfiles" && len(settings.MusicRoots) != 0 || plugin != "musicfs.plugin.decoder.ffmpeg" && (settings.FFmpegPath != "" || settings.FFprobePath != "") {
		return errors.New("instance config does not match pluginId")
	}
	if enabled && plugin == "musicfs.plugin.decoder.ffmpeg" && (settings.FFmpegPath == "" || settings.FFprobePath == "") {
		return errors.New("enabled audio instance requires ffmpegPath and ffprobePath")
	}
	if enabled && plugin == "musicfs.plugin.source.localfiles" && len(settings.MusicRoots) == 0 {
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
