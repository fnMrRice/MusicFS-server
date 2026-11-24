// Package bundledplugin 提供当前构建中各官方插件的实现版本。
package bundledplugin

import (
	_ "embed"
	"encoding/json"
	"strings"
)

//go:embed releases.json
var declarations []byte

// 构建工具以 -ldflags 注入当前提交与工作树对应的插件版本。
var buildVersions string

var releases = loadVersions()

func loadVersions() map[string]string {
	var catalog struct {
		Plugins map[string]struct{ Version string }
	}
	if err := json.Unmarshal(declarations, &catalog); err != nil {
		panic(err)
	}
	versions := make(map[string]string, len(catalog.Plugins))
	for id, plugin := range catalog.Plugins {
		versions[id] = plugin.Version
	}
	for pair := range strings.SplitSeq(buildVersions, ",") {
		if id, version, ok := strings.Cut(pair, "="); ok {
			versions[id] = version
		}
	}
	return versions
}

// Version 返回构建所包含的实现版本，第三方插件由其自身声明版本。
func Version(id string) string { return releases[id] }

var (
	LocalFilesVersion   = Version("musicfs.plugin.source.localfiles")
	FFmpegVersion       = Version("musicfs.plugin.decoder.ffmpeg")
	NCMVersion          = Version("musicfs.plugin.decoder.ncm")
	LocalScraperVersion = Version("musicfs.scraper.local")
	NetEaseVersion      = Version("musicfs.plugin.source.netease")
	QQMusicVersion      = Version("musicfs.plugin.source.qqmusic")
	AppleMusicVersion   = Version("musicfs.plugin.source.applemusic")
	BilibiliVersion     = Version("musicfs.plugin.source.bilibili")
	GainVersion         = Version("musicfs.dsp.gain")
	EqualizerVersion    = Version("musicfs.dsp.equalizer")
	CompressorVersion   = Version("musicfs.dsp.compressor")
	ReverbVersion       = Version("musicfs.dsp.reverb")
	ChannelMixVersion   = Version("musicfs.plugin.decoder.channelmix")
	SQLiteVersion       = Version("musicfs.plugin.storage.sqlite")
	PostgresVersion     = Version("musicfs.plugin.storage.postgres")
	ChromaprintVersion  = Version("musicfs.plugin.analysis.chromaprint")
	FLACVersion         = Version("musicfs.plugin.tags.flac")
	MatchingVersion     = Version("musicfs.plugin.matching.standard")
	ThumbnailVersion    = Version("musicfs.plugin.transform.thumbnail")
)
