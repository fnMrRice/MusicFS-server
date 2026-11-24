package config

import (
	"path/filepath"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"go.yaml.in/yaml/v3"
)

// discoverBuiltin 只从编译内置目录补齐配置，不依赖外部插件程序存在。
func discoverBuiltin(list, node *yaml.Node, id string) (*yaml.Node, error) {
	if node == nil {
		node = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		if err := setNode(node, "pluginId", id); err != nil {
			return nil, err
		}
		list.Content = append(list.Content, node)
		if err := setNode(node, "enabled", true); err != nil {
			return nil, err
		}
	}
	if mappingValue(node, "execution") == nil {
		if err := setNode(node, "execution", "builtin"); err != nil {
			return nil, err
		}
	}
	return node, nil
}

func discoverDefaultInstance(node *yaml.Node, name string, used map[string]bool) error {
	if mappingValue(node, "instances") != nil {
		return nil
	}
	id := instanceid.Canonical(name)
	for used[id] {
		name += "-builtin"
		id = instanceid.Canonical(name)
	}
	used[id] = true
	return setNode(node, "instances", []Instance{{InstanceID: id, Enabled: true}})
}

// NormalizeRuntime 补齐日志默认值，并将旧随包 SQLite/DSP 配置迁移为主进程内执行。
// 不改写磁盘配置，也不影响显式选择的外部存储后端。
func (c *Config) NormalizeRuntime() {
	c.Logging = c.Logging.normalized(c.DataRoot)
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
