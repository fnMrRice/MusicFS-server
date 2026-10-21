package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginrpc"
)

type component struct {
	Program      string   `json:"program"`
	Args         []string `json:"args,omitempty"`
	PluginID     string   `json:"pluginId,omitempty"`
	Required     bool     `json:"required,omitempty"`
	Version      string   `json:"version"`
	Protocol     string   `json:"protocol"`
	Capabilities []string `json:"declaredCapabilities"`
	Requirements []string `json:"requirements"`
}

// This release catalog records official manifests, not a promise that external
// tools, credentials or remote platforms will be available at installation.
func components(splitPlugins bool) []component {
	result := []component{
		{Program: "musicfs", Required: true, Version: "0.1.0", Protocol: "HTTP API v1", Capabilities: []string{"local-library", "playback", "metadata"}, Requirements: []string{"configuration", "administrator token"}},
		{Program: "musicfs-plugin-local", PluginID: "musicfs.plugin.source.localfiles", Version: "0.3.0", Protocol: pluginproto.ProtocolVersion, Capabilities: []string{"library.scan", "metadata.read", "stream.resolve"}, Requirements: []string{"authorized music root"}},
		{Program: "musicfs-plugin-audio-ffmpeg", PluginID: "musicfs.plugin.decoder.ffmpeg", Version: "0.2.0", Protocol: pluginproto.ProtocolVersion, Capabilities: []string{"audio.decode", "audio.dsp", "audio.encode", "audio.mux"}, Requirements: []string{"FFmpeg and ffprobe capability probe", "audio.normalize requires normalization probe"}},
		{Program: "musicfs-plugin-ncm", PluginID: "musicfs.plugin.decoder.ncm", Version: "0.1.0", Protocol: pluginproto.ProtocolVersion, Capabilities: []string{"audio.decrypt"}, Requirements: []string{}},
		{Program: "musicfs-plugin-scraper-local", PluginID: "musicfs.scraper.local", Version: "0.1.0", Protocol: pluginproto.ProtocolVersion, Capabilities: []string{"scrape.search", "scrape.fetch", "lyrics", "artwork"}, Requirements: []string{"authorized host content channel"}},
	}
	for _, platform := range []string{"netease", "qqmusic", "applemusic"} {
		result = append(result, component{Program: "musicfs-plugin-" + platform, PluginID: pluginproto.OnlinePluginID(platform), Version: "0.1.0", Protocol: pluginproto.ProtocolVersion, Capabilities: pluginproto.OnlineCapabilities(platform), Requirements: []string{"host-controlled network", "platform account authorization for online data", "no full audio resolution"}})
	}
	result = append(result,
		component{Program: "musicfs-plugin-storage-sqlite", Required: true, Version: "0.1.0", Protocol: pluginrpc.Protocol, Capabilities: []string{"storage.repositories", "storage.instance-state", "storage.snapshot"}, Requirements: []string{"mandatory persistence worker", "executable SHA-256 verification", "startup fails if unavailable"}},
		component{Program: "musicfs-plugin-dsp-gain", PluginID: "musicfs.dsp.gain", Version: "0.1.0", Protocol: "DSP PCM v1 (MFD1)", Capabilities: []string{"audio.dsp.gain", "stateless-pcm"}, Requirements: []string{"explicit audioDSP chain and plugin configuration", "executable SHA-256", "enabled audio processor", "disabled by default"}},
	)
	var alternatives []component
	for i := 1; i < len(result); i++ {
		item := &result[i]
		selector := strings.TrimPrefix(item.Program, "musicfs-plugin-")
		if splitPlugins && i >= 5 {
			alternative := *item
			alternative.Required = false
			alternative.Requirements = append([]string{"optional standalone alternative to the bundled role"}, item.Requirements...)
			alternatives = append(alternatives, alternative)
			if i <= 7 {
				alternative.Program = "musicfs-plugin-online"
				alternative.Args = []string{"--plugin", selector}
				alternatives = append(alternatives, alternative)
			}
		}
		item.Args = []string{"--plugin", selector}
		item.Program = "musicfs-plugins"
	}
	return append(result, alternatives...)
}

func (b builder) schemaVersion() (int, error) {
	files, err := filepath.Glob(filepath.Join(b.root, "src", "storage", "migrations", "*.sql"))
	if err != nil {
		return 0, err
	}
	for index, path := range files {
		prefix, _, ok := strings.Cut(filepath.Base(path), "_")
		version, err := strconv.Atoi(prefix)
		if !ok || err != nil || version != index+1 {
			return 0, fmt.Errorf("migration sequence is incomplete at %s", filepath.Base(path))
		}
	}
	if len(files) == 0 {
		return 0, fmt.Errorf("database migration sequence is empty")
	}
	return len(files), nil
}
