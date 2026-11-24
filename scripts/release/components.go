package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
	_ "github.com/fnMrRice/MusicFS-server/plugins/dsp/builtin"
	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/version"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	_ "github.com/fnMrRice/MusicFS-server/src/plugins/builtinplugins"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginrpc"
)

type component struct {
	Program      string   `json:"program"`
	Execution    string   `json:"execution,omitempty"`
	Args         []string `json:"args,omitempty"`
	PluginID     string   `json:"pluginId,omitempty"`
	Required     bool     `json:"required,omitempty"`
	Version      string   `json:"version"`
	Protocol     string   `json:"protocol"`
	Capabilities []string `json:"declaredCapabilities"`
	Requirements []string `json:"requirements"`
}

// 发布目录记录内置实现和可选外部入口；工具、凭据及远端平台仍须单独可用。
func components(splitPlugins bool) []component {
	result := []component{
		{Program: "musicfs", Required: true, Version: version.Server, Protocol: "HTTP API v1", Capabilities: []string{"local-library", "playback", "metadata"}, Requirements: []string{"configuration", "administrator token"}},
		{Program: "musicfs-plugin-local", PluginID: "musicfs.plugin.source.localfiles", Version: bundledplugin.LocalFilesVersion, Protocol: pluginproto.ProtocolVersion, Capabilities: []string{"library.scan", "metadata.read", "stream.resolve"}, Requirements: []string{"authorized music root"}},
		{Program: "musicfs-plugin-audio-ffmpeg", PluginID: "musicfs.plugin.decoder.ffmpeg", Version: bundledplugin.FFmpegVersion, Protocol: pluginproto.ProtocolVersion, Capabilities: []string{"audio.decode", "audio.dsp", "audio.dsp.builtin", "audio.encode", "audio.mux"}, Requirements: []string{"FFmpeg and ffprobe capability probe", "audio.normalize requires normalization probe"}},
		{Program: "musicfs-plugin-ncm", PluginID: "musicfs.plugin.decoder.ncm", Version: bundledplugin.NCMVersion, Protocol: pluginproto.ProtocolVersion, Capabilities: []string{"audio.decrypt"}, Requirements: []string{}},
		{Program: "musicfs-plugin-scraper-local", PluginID: "musicfs.scraper.local", Version: bundledplugin.LocalScraperVersion, Protocol: pluginproto.ProtocolVersion, Capabilities: []string{"scrape.search", "scrape.fetch", "lyrics", "artwork"}, Requirements: []string{"authorized host content channel"}},
	}
	onlinePrograms := map[string]bool{}
	for _, descriptor := range onlineplugins.Descriptors() {
		capabilities := pluginproto.OnlineCapabilities(descriptor)
		requirements := []string{"host-controlled network"}
		if descriptor.Authentication == "platformAccountRequired" {
			requirements = append(requirements, "platform account authorization for online data")
		}
		if !slices.Contains(capabilities, "stream.resolve") {
			requirements = append(requirements, "no full audio resolution")
		}
		plugin := onlineplugins.Plugin(descriptor.Platform, onlineplugins.Options{})
		result = append(result, component{Program: "musicfs-plugin-" + descriptor.Platform, PluginID: pluginproto.OnlinePluginID(descriptor.Platform), Version: plugin.Version, Protocol: pluginproto.ProtocolVersion, Capabilities: capabilities, Requirements: requirements})
		onlinePrograms[descriptor.Platform] = true
	}
	result = append(result,
		component{Program: "musicfs-plugin-storage-sqlite", PluginID: "musicfs.plugin.storage.sqlite", Required: true, Version: bundledplugin.SQLiteVersion, Protocol: pluginrpc.Protocol, Capabilities: []string{"storage.repositories", "storage.instance-state", "storage.snapshot"}, Requirements: []string{"mandatory persistence backend", "startup fails if unavailable"}},
		component{Program: "musicfs-plugin-dsp-gain", PluginID: "musicfs.dsp.gain", Version: bundledplugin.GainVersion, Protocol: "DSP PCM v1 (MFD1)", Capabilities: []string{"audio.dsp.gain", "stateless-pcm"}, Requirements: []string{"explicit audioDSP chain and plugin configuration", "enabled audio processor", "disabled by default"}},
	)
	var alternatives []component
	var builtins []component
	for i := 1; i < len(result); i++ {
		item := &result[i]
		selector := strings.TrimPrefix(item.Program, "musicfs-plugin-")
		builtin := *item
		builtin.Program, builtin.Execution = "musicfs", "builtin"
		builtins = append(builtins, builtin)
		item.Required, item.Execution = false, "process"
		item.Requirements = append([]string{"optional external process; requires explicit execution and executable SHA-256"}, item.Requirements...)
		if splitPlugins && (onlinePrograms[selector] || selector == "storage-sqlite" || selector == "dsp-gain") {
			alternative := *item
			alternative.Required = false
			alternative.Requirements = append([]string{"optional standalone alternative to the bundled role"}, item.Requirements...)
			alternatives = append(alternatives, alternative)
			if onlinePrograms[selector] {
				alternative.Program = "musicfs-plugin-online"
				alternative.Args = []string{"--plugin", selector}
				alternatives = append(alternatives, alternative)
			}
		}
		item.Args = []string{"--plugin", selector}
		item.Program = "musicfs-plugins"
	}
	builtins = append(builtins, component{Program: "musicfs", Execution: "builtin", PluginID: "musicfs.plugin.storage.postgres",
		Version: bundledplugin.PostgresVersion, Protocol: "Go interface v1", Capabilities: []string{"storage.repositories", "storage.instance-state", "storage.snapshot"},
		Requirements: []string{"PostgreSQL database configured in storage.options"}})
	requirements := map[string]string{
		"musicfs.plugin.analysis.chromaprint": "enabled fingerprinting and verified fpcalc tool",
		"musicfs.plugin.tags.flac":            "authorized writeback and original-file protection",
		"musicfs.plugin.matching.standard":    "host-validated evidence",
		"musicfs.plugin.transform.thumbnail":  "validated source artwork",
	}
	for _, plugin := range plugincontrol.BuiltinCatalog() {
		required := []string{}
		if requirement := requirements[plugin.PluginID]; requirement != "" {
			required = append(required, requirement)
		}
		builtins = append(builtins, component{Program: "musicfs", Execution: "builtin", Protocol: "Go interface v1",
			PluginID: plugin.PluginID, Version: plugin.Version, Capabilities: plugin.Capabilities, Requirements: required})
	}
	for _, node := range audio.Catalog() {
		if node.PluginVersion == "" || node.Capability == "gain" {
			continue
		}
		builtins = append(builtins, component{Program: "musicfs", Execution: "builtin", Protocol: "Go interface v1",
			PluginID: node.PluginID, Version: node.PluginVersion, Capabilities: []string{"audio.dsp." + node.Name}, Requirements: []string{}})
	}
	return append(append(result, builtins...), alternatives...)
}

func (b builder) schemaVersion() (int, error) {
	files := os.DirFS(filepath.Join(b.root, "plugins", "database", "sqlite", "migrations"))
	entries, err := migrations.LoadFrom(files)
	if err != nil {
		return 0, err
	}
	if len(entries) == 0 {
		return 0, fmt.Errorf("database migration sequence is empty")
	}
	return entries[len(entries)-1].Version, nil
}
