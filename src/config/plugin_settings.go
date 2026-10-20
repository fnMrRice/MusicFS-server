package config

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugindb"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

// LocalSettings is runtime configuration, never deployment authority.
type LocalSettings struct {
	Name           string                       `json:"name"`
	MusicRoots     []MusicRoot                  `json:"musicRoots"`
	Watch          *WatchConfig                 `json:"watch"`
	ImportDefaults *pluginproto.ImportOverrides `json:"importDefaults"`
}

func PluginDatabasePath(c Config) string {
	return filepath.Join(c.DataRoot, "providers", "plugin-config.sqlite")
}

func localSettings(i Instance) LocalSettings {
	return LocalSettings{i.Name, i.Config.MusicRoots, i.Config.Watch, i.ImportDefaults}
}

// AllowedDirectories returns the deployment ceiling. Legacy roots provide a
// restricted compatibility ceiling for configurations not loaded from YAML.
// The loader supplies home Music and saved discovery defaults when permissions
// are omitted; explicit permissions always replace those defaults.
func AllowedDirectories(i Instance) []string {
	if i.Permissions != nil {
		return append([]string{}, i.Permissions.ReadDirectories...)
	}
	paths := []string{}
	for _, r := range i.Config.MusicRoots {
		paths = append(paths, r.Path)
	}
	return paths
}

func directoryWithin(parent, child string) bool {
	lexical, err := filepath.Rel(parent, child)
	if err != nil || lexical != "." && !filepath.IsLocal(lexical) {
		return false
	}
	rel, err := filepath.Rel(canonicalPath(parent), canonicalPath(child))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func RootBoundary(i Instance, path string) (string, error) {
	for _, allowed := range AllowedDirectories(i) {
		if directoryWithin(allowed, path) {
			return allowed, nil
		}
	}
	return "", domain.Fail("ACCESS_DENIED", "Music directory is outside deployment permissions")
}

func validateRootAuthority(i Instance, roots []MusicRoot) error {
	for _, root := range roots {
		allowed := false
		for _, path := range AllowedDirectories(i) {
			if directoryWithin(path, root.Path) {
				allowed = true
				break
			}
		}
		if !allowed {
			return domain.Fail("ACCESS_DENIED", "Music directory is outside the deployment permissions")
		}
	}
	return nil
}

func loadPluginSettings(ctx context.Context, doc *Document) error {
	if _, err := os.Stat(PluginDatabasePath(doc.Config)); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	for pi := range doc.Config.Plugins {
		p := &doc.Config.Plugins[pi]
		if p.PluginID != "musicfs.plugin.source.localfiles" {
			continue
		}
		for ii := range p.Instances {
			i := &p.Instances[ii]
			v, err := plugindb.ReadBackend(ctx, PluginDatabaseBackend(doc.Config), PluginDatabasePath(doc.Config), p.PluginID, i.InstanceID, plugindb.ConfigurationKey)
			if err != nil {
				return err
			}
			if v.Revision == 0 {
				continue
			}
			var settings LocalSettings
			if err := json.Unmarshal(v.Value, &settings); err != nil {
				return fmt.Errorf("invalid stored plugin configuration")
			}
			// Freeze the YAML ceiling before overlaying persisted roots.
			i.Permissions = &Permissions{ReadDirectories: AllowedDirectories(*i)}
			i.Name, i.Config.MusicRoots, i.Config.Watch, i.ImportDefaults = settings.Name, settings.MusicRoots, settings.Watch, settings.ImportDefaults
			i.PendingConfiguration = len(i.Config.MusicRoots) == 0
			// Revocation takes effect on restart without deleting stored settings.
			if validateRootAuthority(*i, i.Config.MusicRoots) != nil {
				i.PendingConfiguration = true
			}
			doc.Revision = fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s:%d", doc.Revision, p.PluginID, i.InstanceID, v.Revision))))
		}
	}
	return nil
}

func prepareLocalSettings(c Config, change Change) (Config, LocalSettings, error) {
	next := cloneConfig(c)
	for pi := range next.Plugins {
		p := &next.Plugins[pi]
		if p.PluginID != change.PluginID {
			continue
		}
		for ii := range p.Instances {
			i := &p.Instances[ii]
			if i.InstanceID != change.InstanceID {
				continue
			}
			s := change.Settings
			i.Permissions = &Permissions{ReadDirectories: AllowedDirectories(*i)}
			if s.FFmpegPath != nil || s.FFprobePath != nil {
				return Config{}, LocalSettings{}, domain.Fail("INVALID_ARGUMENT", "Invalid local provider settings")
			}
			if s.Name != nil {
				i.Name = *s.Name
			}
			if s.Watch != nil {
				i.Config.Watch = s.Watch
			}
			if s.ImportDefaults != nil {
				i.ImportDefaults = s.ImportDefaults.Clone()
			}
			if s.MusicRoots != nil {
				roots := []MusicRoot{}
				selectedPaths := map[string]bool{}
				for _, input := range *s.MusicRoots {
					r := MusicRoot{ID: input.ID, Name: input.Name}
					if input.Path != nil {
						r.Path = *input.Path
					}
					for _, old := range i.Config.MusicRoots {
						if old.ID != r.ID {
							continue
						}
						if input.Path == nil {
							r.Path = old.Path
						}
						if directoryIdentity(r.Path) != directoryIdentity(old.Path) {
							return Config{}, LocalSettings{}, domain.Fail("INVALID_ARGUMENT", "Existing root IDs cannot change directories")
						}
					}
					if !filepath.IsAbs(r.Path) {
						return Config{}, LocalSettings{}, domain.Fail("INVALID_ARGUMENT", "Music directories require absolute server paths")
					}
					if err := validateRootAuthority(*i, []MusicRoot{r}); err != nil {
						return Config{}, LocalSettings{}, err
					}
					if input.Path != nil {
						root, err := OpenAllowedDirectory(*i, r.Path)
						if err != nil {
							return Config{}, LocalSettings{}, err
						}
						root.Close()
					}
					identity := directoryIdentity(r.Path)
					if selectedPaths[identity] {
						return Config{}, LocalSettings{}, domain.Fail("INVALID_ARGUMENT", "Music directory is already selected")
					}
					selectedPaths[identity] = true
					roots = append(roots, r)
				}
				i.Config.MusicRoots = roots
			}
			i.PendingConfiguration = len(i.Config.MusicRoots) == 0
			if err := validateRootAuthority(*i, i.Config.MusicRoots); err != nil {
				return Config{}, LocalSettings{}, err
			}
			if err := next.Validate(); err != nil {
				return Config{}, LocalSettings{}, domain.Fail("INVALID_ARGUMENT", "Invalid plugin configuration")
			}
			return next, localSettings(*i), nil
		}
	}
	return Config{}, LocalSettings{}, domain.Fail("NOT_FOUND", "Plugin instance not found")
}
