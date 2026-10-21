package config

import (
	"errors"

	"go.yaml.in/yaml/v3"
)

const MaxWatchedInstances = 8

// WatchConfig controls host scheduling only; it never grants additional roots.
type WatchConfig struct {
	Enabled           bool `yaml:"enabled" json:"enabled"`
	DebounceMs        int  `yaml:"debounceMs" json:"debounceMs"`
	MaxDelayMs        int  `yaml:"maxDelayMs" json:"maxDelayMs"`
	FullRescanSeconds int  `yaml:"fullRescanSeconds" json:"fullRescanSeconds"`
}

func DefaultWatch() WatchConfig {
	return WatchConfig{DebounceMs: 1000, MaxDelayMs: 10000, FullRescanSeconds: 3600}
}

func (w *WatchConfig) UnmarshalYAML(node *yaml.Node) error {
	type plain WatchConfig
	v := plain(DefaultWatch())
	if err := node.Decode(&v); err != nil {
		return err
	}
	*w = WatchConfig(v)
	return nil
}

func (w WatchConfig) Validate() error {
	if w.DebounceMs < 100 || w.DebounceMs > 30000 || w.MaxDelayMs < w.DebounceMs || w.MaxDelayMs > 60000 || w.FullRescanSeconds < 30 || w.FullRescanSeconds > 86400 {
		return errors.New("invalid watch timing limits")
	}
	return nil
}

func watchShape() fieldShape {
	return mappingShape(map[string]fieldShape{"enabled": boolShape, "debounceMs": intShape, "maxDelayMs": intShape, "fullRescanSeconds": intShape})
}
