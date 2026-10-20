package config

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugindb"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

const defaultDirectoriesKey = "host-default-read-directories"

// InitializeLocalDirectories runs under the server data lock, before plugins
// start. The saved snapshot includes empty discovery results. Configuration
// reads only load it; they never probe drives or recreate removed music roots.
func InitializeLocalDirectories(ctx context.Context, c Config) (Config, error) {
	return initializeLocalDirectories(ctx, c, discoverMusicDirectories)
}

func initializeLocalDirectories(ctx context.Context, c Config, discover func() []string) (Config, error) {
	c = cloneConfig(c)
	var discovered []string
	probed := false
	for pi := range c.Plugins {
		p := &c.Plugins[pi]
		if p.PluginID != "musicfs.plugin.source.localfiles" || !p.Enabled {
			continue
		}
		for ii := range p.Instances {
			i := &p.Instances[ii]
			if !i.Enabled || i.Permissions != nil && !i.defaultReadDirectories {
				continue
			}
			db, err := plugindb.Open(ctx, PluginDatabasePath(c), p.PluginID, i.InstanceID)
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
					if !overlaps(path, c.DataRoot) && !overlaps(path, c.MediaRoot) && !overlaps(path, c.CacheRoot) {
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
	_, statErr := os.Stat(PluginDatabasePath(*c))
	if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	for pi := range c.Plugins {
		p := &c.Plugins[pi]
		if p.PluginID != "musicfs.plugin.source.localfiles" {
			continue
		}
		for ii := range p.Instances {
			i := &p.Instances[ii]
			if i.Permissions != nil {
				continue
			}
			var v pluginproto.StateValue
			if statErr == nil {
				var err error
				v, err = plugindb.Read(ctx, PluginDatabasePath(*c), p.PluginID, i.InstanceID, defaultDirectoriesKey)
				if err != nil {
					return err
				}
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
	for _, path := range append([]string{defaultUserMusicDirectory()}, saved.ReadDirectories...) {
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
			if p.PluginID == "musicfs.plugin.source.localfiles" && i.defaultDirectoryRevision > 0 {
				revision = fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s:%s:%d", revision, p.PluginID, i.InstanceID, defaultDirectoriesKey, i.defaultDirectoryRevision))))
			}
		}
	}
	return revision
}
