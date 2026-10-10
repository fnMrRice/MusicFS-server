package config

import "errors"

type WriteBackRoot struct {
	InstanceID string   `yaml:"instanceId" json:"instanceId"`
	RootID     string   `yaml:"rootId" json:"rootId"`
	Fields     []string `yaml:"fields" json:"fields"`
}

type WriteBackConfig struct {
	Enabled        bool            `yaml:"enabled" json:"enabled"`
	AllowOverwrite bool            `yaml:"allowOverwrite" json:"allowOverwrite"`
	MaxFileBytes   int64           `yaml:"maxFileBytes" json:"maxFileBytes"`
	Roots          []WriteBackRoot `yaml:"roots" json:"roots"`
}

func (c Config) validateWriteBack() error {
	p := c.WriteBack
	if p.MaxFileBytes < 46 || p.MaxFileBytes > 1<<40 || len(p.Roots) > 256 || (p.Enabled && len(p.Roots) == 0) {
		return errors.New("invalid writeBack limits or missing explicit root grants")
	}
	type instanceGrant struct {
		enabled bool
		roots   map[string]bool
	}
	instances := map[string]instanceGrant{}
	for _, plugin := range c.Plugins {
		if plugin.PluginID != "musicfs.localfiles" {
			continue
		}
		for _, instance := range plugin.Instances {
			g := instanceGrant{enabled: plugin.Enabled && instance.Enabled, roots: map[string]bool{}}
			for _, root := range instance.Config.MusicRoots {
				g.roots[root.ID] = true
			}
			instances[instance.InstanceID] = g
		}
	}
	seen := map[string]bool{}
	for _, root := range p.Roots {
		grant, ok := instances[root.InstanceID]
		key := root.InstanceID + "\x00" + root.RootID
		if !ok || !grant.roots[root.RootID] || seen[key] || (p.Enabled && !grant.enabled) {
			return errors.New("writeBack grants require unique configured local roots and enabled dependencies when active")
		}
		seen[key] = true
		if len(root.Fields) < 1 || len(root.Fields) > 2 {
			return errors.New("writeBack root must authorize one or two fields")
		}
		fields := map[string]bool{}
		for _, field := range root.Fields {
			if (field != "lyrics" && field != "artwork") || fields[field] {
				return errors.New("invalid or duplicate writeBack field")
			}
			fields[field] = true
		}
	}
	return nil
}
