package config

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/core/logging"
	"go.yaml.in/yaml/v3"
)

// LoggingConfig 保存启动日志配置；运行时等级调整不回写部署文件。
type LoggingConfig struct {
	Directory string            `yaml:"directory,omitempty" json:"-"`
	Level     string            `yaml:"level" json:"level"`
	Plugins   map[string]string `yaml:"plugins,omitempty" json:"plugins"`
}

func (c LoggingConfig) Validate() error {
	c = c.normalized("")
	if strings.TrimSpace(c.Directory) == "" || strings.ContainsRune(c.Directory, 0) {
		return errors.New("logging.directory must be a nonempty path")
	}
	if logging.ValidateLevel(c.Level) != nil {
		return errors.New("logging.level must be trace, debug, info, warn or error")
	}
	if len(c.Plugins) > 128 {
		return errors.New("logging.plugins cannot exceed 128 entries")
	}
	for id, level := range c.Plugins {
		if !logging.ValidPluginID(id) || logging.ValidateLevel(level) != nil {
			return errors.New("logging.plugins requires valid plugin identifiers and log levels")
		}
	}
	return nil
}

func (c LoggingConfig) normalized(dataRoot string) LoggingConfig {
	if c.Directory == "" {
		c.Directory = filepath.Join(dataRoot, "logs")
	}
	if c.Level == "" {
		c.Level = "info"
	}
	if c.Plugins == nil {
		c.Plugins = map[string]string{}
	}
	return c
}

func loggingShape() fieldShape {
	return mappingShape(map[string]fieldShape{
		"directory": stringShape,
		"level":     stringShape,
		"plugins":   {tag: "!!map", values: &stringShape},
	})
}

func validateLoggingNodes(root *yaml.Node) error {
	if node := yamlField(root, "logging"); node != nil {
		if directory := yamlField(node, "directory"); directory != nil && strings.TrimSpace(directory.Value) == "" {
			return nodeError(directory, "config.logging.directory", "expected a nonempty path")
		}
		if level := yamlField(node, "level"); level != nil && level.Value == "" {
			return nodeError(level, "config.logging.level", "expected a log level")
		}
	}
	return nil
}
