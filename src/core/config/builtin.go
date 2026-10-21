package config

import (
	"path/filepath"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"go.yaml.in/yaml/v3"
)

// discoverBuiltin 只从编译内置目录补齐配置，不依赖外部插件程序存在。
func discoverBuiltin(list, node *yaml.Node, id, legacy string, used map[string]bool) error {
	if node == nil {
		node = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		if err := setNode(node, "pluginId", id); err != nil {
			return err
		}
		list.Content = append(list.Content, node)
		if err := setNode(node, "enabled", true); err != nil {
			return err
		}
	}
	for _, field := range []struct {
		key   string
		value any
	}{{"execution", "builtin"}} {
		if mappingValue(node, field.key) == nil {
			if err := setNode(node, field.key, field.value); err != nil {
				return err
			}
		}
	}
	if mappingValue(node, "instances") == nil {
		id := instanceid.Canonical(legacy)
		for used[id] {
			legacy += "-builtin"
			id = instanceid.Canonical(legacy)
		}
		used[id] = true
		if err := setNode(node, "instances", []Instance{{InstanceID: id, Enabled: true}}); err != nil {
			return err
		}
	}
	return nil
}

// NormalizeRuntime 将旧随包 SQLite/DSP 配置迁移为主进程内执行。
// 不改写磁盘配置，也不影响显式选择的外部存储后端。
func (c *Config) NormalizeRuntime() {
	if c.Storage.Backend == "plugin" && c.Storage.Execution != "process" {
		name := strings.TrimSuffix(strings.ToLower(filepath.Base(c.Storage.Executable)), ".exe")
		if name == "musicfs-plugin-storage-sqlite" || name == "musicfs-plugin-sqlite" || name == "musicfs-plugins" && onlineSelectorArgs(c.Storage.Args, "storage-sqlite") {
			c.Storage.Backend = "sqlite"
			c.Storage.Executable, c.Storage.SHA256, c.Storage.Args = "", "", nil
		}
	}
	for i := range c.AudioDSP.Plugins {
		if c.AudioDSP.Plugins[i].Name == "musicfs.dsp.gain" {
			c.AudioDSP.Plugins[i].Builtin = true
		}
	}
}
