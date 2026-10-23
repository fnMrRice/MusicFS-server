package config

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/musicdirs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugindb"
)

const defaultDirectoriesKey = "host-default-read-directories"

// InitializeLocalDirectories runs under the server data lock, before plugins
// start. The saved snapshot includes empty discovery results. Configuration
// reads only load it; they never probe drives or recreate removed music roots.
// The composition root supplies system discovery; saved and explicit permissions
// skip that dependency, including when a saved discovery result is empty.
func InitializeLocalDirectories(ctx context.Context, c Config, discover func() []string) (Config, error) {
	c = cloneConfig(c)
	var discovered []string
	probed := false
	for pi := range c.Plugins {
		p := &c.Plugins[pi]
		if !pluginconfig.SupportsDirectories(p.PluginID) || !p.Enabled {
			continue
		}
		for ii := range p.Instances {
			i := &p.Instances[ii]
			if !i.Enabled || i.Permissions != nil && !i.defaultReadDirectories {
				continue
			}
			db, err := plugindb.OpenBackend(ctx, PluginDatabaseBackend(c), PluginDatabasePath(c), p.PluginID, i.InstanceID)
			if err != nil {
				return Config{}, err
			}
			v, err := db.Get(ctx, defaultDirectoriesKey)
			if err == nil && v.Revision == 0 {
				if !probed {
					discovered, probed = discover(), true
				}
				paths := []string{}
				for _, path := range discovered {
					if !overlaps(path, c.DataRoot) && !overlaps(path, c.MediaRoot) && !overlaps(path, c.CacheRoot) && !overlaps(path, c.Logging.Directory) {
						paths = append(paths, path)
					}
				}
				value, _ := json.Marshal(Permissions{ReadDirectories: paths})
				v, err = db.CompareAndSet(ctx, pluginproto.StateRequest{Key: defaultDirectoriesKey, Value: value})
				var conflict *domain.Error
				if errors.As(err, &conflict) && conflict.Code == "PRECONDITION_FAILED" {
					v, err = db.Get(ctx, defaultDirectoriesKey)
				}
			}
			db.Close()
			if err != nil {
				return Config{}, err
			}
			if err := applyLocalDirectoryDefaults(i, v); err != nil {
				return Config{}, err
			}
		}
	}
	return c, nil
}

func loadLocalDirectoryDefaults(ctx context.Context, c *Config) error {
	for pi := range c.Plugins {
		p := &c.Plugins[pi]
		if !pluginconfig.SupportsDirectories(p.PluginID) {
			continue
		}
		for ii := range p.Instances {
			i := &p.Instances[ii]
			if i.Permissions != nil {
				continue
			}
			v, err := plugindb.ReadBackend(ctx, PluginDatabaseBackend(*c), PluginDatabasePath(*c), p.PluginID, i.InstanceID, defaultDirectoriesKey)
			if err != nil {
				return err
			}
			if err := applyLocalDirectoryDefaults(i, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func applyLocalDirectoryDefaults(i *Instance, v pluginproto.StateValue) error {
	var saved Permissions
	if v.Revision > 0 {
		if err := json.Unmarshal(v.Value, &saved); err != nil || len(saved.ReadDirectories) > 256 {
			return fmt.Errorf("invalid stored default directory permissions")
		}
		for _, path := range saved.ReadDirectories {
			if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
				return fmt.Errorf("invalid stored default directory path")
			}
		}
	}
	paths := AllowedDirectories(*i)
	seen := map[string]bool{}
	key := func(path string) string {
		path = canonicalPath(path)
		if runtime.GOOS == "windows" {
			path = strings.ToLower(path)
		}
		return path
	}
	for _, path := range paths {
		seen[key(path)] = true
	}
	for _, path := range append([]string{musicdirs.Default()}, saved.ReadDirectories...) {
		if path != "" && len(paths) < 256 && !seen[key(path)] {
			paths = append(paths, path)
			seen[key(path)] = true
		}
	}
	i.Permissions = &Permissions{ReadDirectories: paths}
	i.defaultReadDirectories, i.defaultDirectoryRevision = true, v.Revision
	return nil
}

func localDirectoryRevision(c Config, revision string) string {
	for _, p := range c.Plugins {
		for _, i := range p.Instances {
			if pluginconfig.SupportsDirectories(p.PluginID) && i.defaultDirectoryRevision > 0 {
				revision = fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s:%s:%d", revision, p.PluginID, i.InstanceID, defaultDirectoriesKey, i.defaultDirectoryRevision))))
			}
		}
	}
	return revision
}
