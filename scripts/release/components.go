package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/migrations"
	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/version"
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
		{Program: "musicfs-plugin-local", PluginID: "musicfs.plugin.source.localfiles", Version: "0.3.0", Protocol: pluginproto.ProtocolVersion, Capabilities: []string{"library.scan", "metadata.read", "stream.resolve"}, Requirements: []string{"authorized music root"}},
		{Program: "musicfs-plugin-audio-ffmpeg", PluginID: "musicfs.plugin.decoder.ffmpeg", Version: bundledplugin.FFmpegVersion, Protocol: pluginproto.ProtocolVersion, Capabilities: []string{"audio.decode", "audio.dsp", "audio.dsp.builtin", "audio.encode", "audio.mux"}, Requirements: []string{"FFmpeg and ffprobe capability probe", "audio.normalize requires normalization probe"}},
		{Program: "musicfs-plugin-ncm", PluginID: "musicfs.plugin.decoder.ncm", Version: "0.1.0", Protocol: pluginproto.ProtocolVersion, Capabilities: []string{"audio.decrypt"}, Requirements: []string{}},
		{Program: "musicfs-plugin-scraper-local", PluginID: "musicfs.scraper.local", Version: "0.1.0", Protocol: pluginproto.ProtocolVersion, Capabilities: []string{"scrape.search", "scrape.fetch", "lyrics", "artwork"}, Requirements: []string{"authorized host content channel"}},
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
		component{Program: "musicfs-plugin-storage-sqlite", Required: true, Version: "0.1.0", Protocol: pluginrpc.Protocol, Capabilities: []string{"storage.repositories", "storage.instance-state", "storage.snapshot"}, Requirements: []string{"mandatory persistence backend", "startup fails if unavailable"}},
		component{Program: "musicfs-plugin-dsp-gain", PluginID: "musicfs.dsp.gain", Version: "0.1.0", Protocol: "DSP PCM v1 (MFD1)", Capabilities: []string{"audio.dsp.gain", "stateless-pcm"}, Requirements: []string{"explicit audioDSP chain and plugin configuration", "enabled audio processor", "disabled by default"}},
	)
	var alternatives []component
	var builtins []component
	for i := 1; i < len(result); i++ {
		item := &result[i]
		selector := strings.TrimPrefix(item.Program, "musicfs-plugin-")
		builtin := *item
		builtin.Program, builtin.Execution = "musicfs", "builtin"
		if selector == "storage-sqlite" {
			builtin.PluginID = "musicfs.plugin.storage.sqlite"
		}
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
	for _, item := range []component{
		{PluginID: "musicfs.plugin.analysis.chromaprint", Version: bundledplugin.ChromaprintVersion, Capabilities: []string{"fingerprint.chromaprint"}, Requirements: []string{"enabled fingerprinting and verified fpcalc tool"}},
		{PluginID: "musicfs.plugin.tags.flac", Version: bundledplugin.FLACVersion, Capabilities: []string{"tags.flac.rewrite"}, Requirements: []string{"authorized writeback and original-file protection"}},
		{PluginID: "musicfs.plugin.matching.standard", Version: bundledplugin.MatchingVersion, Capabilities: []string{"matching.candidates"}, Requirements: []string{"host-validated evidence"}},
		{PluginID: "musicfs.plugin.transform.thumbnail", Version: bundledplugin.ThumbnailVersion, Capabilities: []string{"artwork.thumbnail256"}, Requirements: []string{"validated source artwork"}},
	} {
		item.Program, item.Execution, item.Protocol = "musicfs", "builtin", "Go interface v1"
		builtins = append(builtins, item)
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
