package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"go.yaml.in/yaml/v3"
)

// Discovery uses a fixed catalog, never PATH or arbitrary directory entries.
var bundledPlugins = []struct{ id, selector, instance, program string }{
	{"musicfs.plugin.source.localfiles", "local", "local", ""},
	{"musicfs.plugin.decoder.ffmpeg", "audio-ffmpeg", "audio-default", ""},
	{"musicfs.plugin.decoder.ncm", "ncm", "ncm-default", ""},
	{"musicfs.scraper.local", "scraper-local", "scraper-local", ""},
	{"musicfs.plugin.source.netease", "netease", "netease-public", "musicfs-plugin-netease"},
	{"musicfs.plugin.source.qqmusic", "qqmusic", "qq-public", "musicfs-plugin-qqmusic"},
	{"musicfs.plugin.source.applemusic", "applemusic", "apple-public", "musicfs-plugin-applemusic"},
}

func bundlePath(root *yaml.Node, base string) string {
	if node := mappingValue(root, "pluginBundle"); node != nil {
		if node.Tag != "!!str" || node.Value == "" {
			return ""
		}
		if filepath.IsAbs(node.Value) {
			return filepath.Clean(node.Value)
		}
		return filepath.Join(base, node.Value)
	}
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	name := "musicfs-plugins"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(executable), name)
}

// Expand only the in-memory YAML tree. Reading does not write a configuration;
// a later explicit settings change persists the expanded tree with its flags.
func discoverBundle(root *yaml.Node, base string) error {
	bundle := bundlePath(root, base)
	if bundle == "" {
		return nil
	}
	list := mappingValue(root, "plugins")
	if list == nil {
		if err := setNode(root, "plugins", []Plugin{}); err != nil {
			return err
		}
		list = mappingValue(root, "plugins")
	}
	if list.Kind != yaml.SequenceNode {
		return nil // The ordinary shape validator reports malformed input.
	}
	used := map[string]bool{}
	for _, p := range list.Content {
		if instances := mappingValue(p, "instances"); instances != nil {
			for _, i := range instances.Content {
				if id := mappingValue(i, "instanceId"); id != nil {
					used[strings.ToLower(id.Value)] = true
				}
			}
		}
	}
	for _, entry := range bundledPlugins {
		path := bundle
		args := []string{"--plugin", entry.selector}
		if entry.program != "" && !regularExecutable(path) {
			path = standalonePath(bundle, entry.program)
			args = []string{}
			if !regularExecutable(path) {
				path = standalonePath(bundle, "musicfs-plugin-online")
				args = []string{"--plugin", entry.selector}
			}
		}
		if !regularExecutable(path) {
			continue
		}
		configuredPath := path
		if relative, err := filepath.Rel(base, path); err == nil {
			configuredPath = relative
		}
		var node *yaml.Node
		for _, p := range list.Content {
			if id := mappingValue(p, "pluginId"); id != nil && id.Value == entry.id {
				node = p
				break
			}
		}
		if node == nil {
			node = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			if err := setNode(node, "pluginId", entry.id); err != nil {
				return err
			}
			list.Content = append(list.Content, node)
		}
		if executable := mappingValue(node, "executable"); executable != nil {
			candidate := executable.Value
			if !filepath.IsAbs(candidate) {
				candidate = filepath.Join(base, candidate)
			}
			fallback := entry.program != "" && canonicalPath(candidate) == canonicalPath(standalonePath(bundle, entry.program)) &&
				!regularExecutable(candidate)
			if fallback {
				// A digest pins the selected program, not its replacement.
				rawArgs := mappingValue(node, "args")
				approvedArgs := rawArgs == nil || (rawArgs.Kind == yaml.SequenceNode && len(rawArgs.Content) == 0)
				if digest := mappingValue(node, "sha256"); (digest != nil && digest.Value != "") || !approvedArgs {
					continue
				}
				if err := setNode(node, "executable", configuredPath); err != nil {
					return err
				}
				if err := setNode(node, "args", args); err != nil {
					return err
				}
			} else if canonicalPath(candidate) != canonicalPath(path) {
				fullBundle := canonicalPath(candidate) == canonicalPath(bundle)
				standalone := entry.program != "" && canonicalPath(candidate) == canonicalPath(standalonePath(bundle, entry.program))
				unified := entry.program != "" && canonicalPath(candidate) == canonicalPath(standalonePath(bundle, "musicfs-plugin-online"))
				if !regularExecutable(candidate) || (!fullBundle && !standalone && !unified) {
					continue // Explicit external executables retain their old semantics.
				}
				// Explicit official executables retain their chosen form.
				configuredPath = candidate
				if relative, err := filepath.Rel(base, candidate); err == nil {
					configuredPath = relative
				}
				args = []string{}
				if fullBundle || unified {
					args = []string{"--plugin", entry.selector}
				}
			}
		}
		defaults := []struct {
			key   string
			value any
		}{
			{"executable", configuredPath}, {"args", args}, {"enabled", true},
		}
		for _, field := range defaults {
			if mappingValue(node, field.key) == nil {
				if err := setNode(node, field.key, field.value); err != nil {
					return err
				}
			}
		}
		if mappingValue(node, "instances") == nil {
			legacy := entry.instance
			id := instanceid.Canonical(legacy)
			for used[id] {
				legacy += "-builtin"
				id = instanceid.Canonical(legacy)
			}
			used[strings.ToLower(id)] = true
			instances := []Instance{{InstanceID: id, Enabled: true}}
			if err := setNode(node, "instances", instances); err != nil {
				return err
			}
		}
		for _, i := range mappingValue(node, "instances").Content {
			if i.Kind == yaml.MappingNode && mappingValue(i, "enabled") == nil {
				if err := setNode(i, "enabled", true); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func markPendingConfiguration(c *Config, bundle string) {
	if bundle == "" {
		return
	}
	for pi := range c.Plugins {
		p := &c.Plugins[pi]
		isBundle := canonicalPath(p.Executable) == canonicalPath(bundle)
		platform := OnlinePlatform(p.PluginID)
		isStandalone := platform != "" && canonicalPath(p.Executable) == canonicalPath(standalonePath(bundle, "musicfs-plugin-"+platform))
		isOnlineBundle := platform != "" && canonicalPath(p.Executable) == canonicalPath(standalonePath(bundle, "musicfs-plugin-online"))
		if !isBundle && !isStandalone && !isOnlineBundle {
			continue
		}
		for ii := range p.Instances {
			i := &p.Instances[ii]
			i.PendingConfiguration = !regularExecutable(p.Executable)
			if platform != "" && (isBundle || isOnlineBundle) && !onlineSelectorArgs(p.Args, platform) {
				i.PendingConfiguration = true
			}
			switch p.PluginID {
			case "musicfs.plugin.source.localfiles":
				i.PendingConfiguration = i.PendingConfiguration || len(i.Config.MusicRoots) == 0
				for _, root := range i.Config.MusicRoots {
					info, err := os.Stat(root.Path)
					i.PendingConfiguration = i.PendingConfiguration || err != nil || !info.IsDir()
				}
			case "musicfs.plugin.decoder.ffmpeg":
				for _, path := range []string{i.Config.FFmpegPath, i.Config.FFprobePath} {
					info, err := os.Stat(path)
					i.PendingConfiguration = i.PendingConfiguration || err != nil || !info.Mode().IsRegular()
				}
			}
		}
	}
}

func standalonePath(bundle, program string) string {
	if strings.EqualFold(filepath.Ext(bundle), ".exe") {
		program += ".exe"
	}
	return filepath.Join(filepath.Dir(bundle), program)
}

func regularExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func onlineSelectorArgs(args []string, platform string) bool {
	return (len(args) == 1 && args[0] == "--plugin="+platform) ||
		(len(args) == 2 && args[0] == "--plugin" && args[1] == platform)
}

func (p Plugin) CanStart(i Instance) bool {
	return p.Enabled && i.Enabled && !i.PendingConfiguration
}

func (p Plugin) Pending(i Instance) bool {
	return p.Enabled && i.Enabled && i.PendingConfiguration
}
