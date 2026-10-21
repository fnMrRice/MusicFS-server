package config

import (
	"encoding/hex"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspchain"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugindb"
)

// StorageConfig is deployment authority, available before any database is opened.
type StorageConfig struct {
	Backend    string            `yaml:"backend,omitempty" json:"backend,omitempty"`
	Options    map[string]string `yaml:"options,omitempty" json:"-"`
	Executable string            `yaml:"executable,omitempty" json:"-"`
	SHA256     string            `yaml:"sha256,omitempty" json:"-"`
	Args       []string          `yaml:"args,omitempty" json:"-"`
}

// AudioDSPConfig is pinned at startup; active playback retains its processor.
type AudioDSPConfig struct {
	Chain   []audio.NodeSpec   `yaml:"chain,omitempty" json:"chain,omitempty"`
	Plugins []dspworker.Config `yaml:"plugins,omitempty" json:"-"`
}

func PluginDatabaseBackend(c Config) plugindb.BackendConfig {
	name := "sqlite"
	if c.Storage.Backend == "plugin" {
		name = "plugin"
	}
	return plugindb.BackendConfig{Name: name, Executable: c.Storage.Executable,
		SHA256: c.Storage.SHA256, Args: append([]string(nil), c.Storage.Args...)}
}

// SameStartupSettings prevents a reloaded YAML file from mixing a new storage
// configuration with the already open database and its data-directory lock.
func SameStartupSettings(a, b Config) bool {
	if a.Storage.Backend == "" {
		a.Storage.Backend = "sqlite"
	}
	if b.Storage.Backend == "" {
		b.Storage.Backend = "sqlite"
	}
	return a.DataRoot == b.DataRoot && a.MediaRoot == b.MediaRoot && a.CacheRoot == b.CacheRoot &&
		reflect.DeepEqual(a.Storage, b.Storage) && reflect.DeepEqual(a.AudioDSP, b.AudioDSP)
}

func (s StorageConfig) Validate() error {
	if s.Backend != "" && !regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`).MatchString(s.Backend) {
		return fmt.Errorf("invalid storage backend identifier")
	}
	if len(s.Options) > 32 {
		return fmt.Errorf("too many storage options")
	}
	for key, value := range s.Options {
		if !validConfigID(key) || len(value) > 4096 || strings.ContainsRune(value, 0) {
			return fmt.Errorf("invalid storage option")
		}
	}
	if len(s.Args) > 64 {
		return fmt.Errorf("too many storage plugin arguments")
	}
	for _, arg := range s.Args {
		if len(arg) > 4096 || strings.ContainsRune(arg, 0) {
			return fmt.Errorf("invalid storage plugin argument")
		}
	}
	if s.Backend == "plugin" {
		hash, err := hex.DecodeString(s.SHA256)
		if s.Executable == "" || strings.ContainsRune(s.Executable, 0) || err != nil || len(hash) != 32 {
			return fmt.Errorf("storage plugin requires an executable and SHA-256")
		}
	} else if s.Executable != "" || s.SHA256 != "" || len(s.Args) != 0 {
		return fmt.Errorf("storage executable settings require the plugin backend")
	}
	return nil
}

func (c AudioDSPConfig) Validate() error {
	return dspchain.Validate(c.Chain, c.Plugins)
}

func storageShape() fieldShape {
	return mappingShape(map[string]fieldShape{
		"backend": stringShape, "executable": stringShape, "sha256": stringShape,
		"args": sequenceShape(stringShape), "options": {tag: "!!map", values: &stringShape},
	})
}

func audioDSPShape() fieldShape {
	number := fieldShape{tag: "number"}
	return mappingShape(map[string]fieldShape{
		"chain": sequenceShape(mappingShape(map[string]fieldShape{
			"name": stringShape, "version": intShape, "params": {tag: "!!map", values: &number},
		}, "name", "version")),
		"plugins": sequenceShape(mappingShape(map[string]fieldShape{
			"name": stringShape, "executable": stringShape, "sha256": stringShape,
			"args": sequenceShape(stringShape), "timeoutMs": intShape,
		}, "name", "executable", "sha256")),
	})
}
