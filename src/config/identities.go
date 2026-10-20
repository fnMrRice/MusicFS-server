package config

import (
	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/pluginid"
	"go.yaml.in/yaml/v3"
)

// NormalizeIdentities upgrades deployment identities without changing aliases,
// root IDs, account IDs, or any content identity.
func (c *Config) NormalizeIdentities() {
	for pi := range c.Plugins {
		p := &c.Plugins[pi]
		p.PluginID = pluginid.Canonical(p.PluginID)
		for ii := range p.Instances {
			i := &p.Instances[ii]
			i.InstanceID = instanceid.Canonical(i.InstanceID)
		}
	}
	for i := range c.WriteBack.Roots {
		c.WriteBack.Roots[i].InstanceID = instanceid.Canonical(c.WriteBack.Roots[i].InstanceID)
	}
	for i, id := range c.Metadata.Automatic.InstancePriority {
		c.Metadata.Automatic.InstancePriority[i] = instanceid.Canonical(id)
	}
}

// Normalize the AST before bundle discovery, so a legacy ID cannot cause a
// second built-in plugin to be discovered. Raw bytes remain the revision source.
func normalizeIdentityNodes(root *yaml.Node) {
	normalizeString := func(node *yaml.Node, convert func(string) string) {
		if node != nil && node.Kind == yaml.ScalarNode && node.Tag == "!!str" {
			node.Value = convert(node.Value)
		}
	}
	if plugins := mappingValue(root, "plugins"); plugins != nil && plugins.Kind == yaml.SequenceNode {
		for _, p := range plugins.Content {
			normalizeString(mappingValue(p, "pluginId"), pluginid.Canonical)
			if instances := mappingValue(p, "instances"); instances != nil && instances.Kind == yaml.SequenceNode {
				for _, i := range instances.Content {
					normalizeString(mappingValue(i, "instanceId"), instanceid.Canonical)
				}
			}
		}
	}
	if wb := mappingValue(root, "writeBack"); wb != nil {
		if roots := mappingValue(wb, "roots"); roots != nil && roots.Kind == yaml.SequenceNode {
			for _, r := range roots.Content {
				normalizeString(mappingValue(r, "instanceId"), instanceid.Canonical)
			}
		}
	}
	if metadata := mappingValue(root, "metadata"); metadata != nil {
		if automatic := mappingValue(metadata, "automatic"); automatic != nil {
			if priority := mappingValue(automatic, "instancePriority"); priority != nil && priority.Kind == yaml.SequenceNode {
				for _, id := range priority.Content {
					normalizeString(id, instanceid.Canonical)
				}
			}
		}
	}
}
