package pluginconfig

import (
	"github.com/fnMrRice/MusicFS-server/plugins/source/localfiles/descriptor"
	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

// LocalFiles 返回官方本地文件插件声明，主要供显式装配和契约夹具使用。
func LocalFiles() *pluginproto.Configuration { return descriptor.Configuration() }

// 官方声明集中在装配层；新增可信插件调用 Register，不修改业务分支。
func init() {
	declarations := []Declaration{
		{ID: "musicfs.plugin.source.localfiles", Role: "provider", SourceType: pluginproto.SourceLocal, Directories: true, Configuration: descriptor.Configuration},
		{ID: "musicfs.plugin.decoder.ffmpeg", Role: "audio", AudioTools: true, AudioProcessing: true, Settings: true},
		{ID: "musicfs.plugin.decoder.ncm", Role: "audio", Settings: true},
		{ID: "musicfs.scraper.local", Role: "scraper", SupportsMultipleInstances: true},
	}
	for _, online := range onlineplugins.Descriptors() {
		declarations = append(declarations, Declaration{ID: online.Plugin.ID, Role: "provider", SourceType: online.SourceType, Platform: online.Platform, SupportsMultipleInstances: true, Identity: online.Plugin, Version: onlineplugins.Plugin(online.Platform, onlineplugins.Options{}).Version})
	}
	for _, d := range declarations {
		if d.Version == "" {
			d.Version = bundledplugin.Version(d.ID)
		}
		if d.Identity == nil {
			d.Identity = builtinIdentity(d.ID)
		}
		if err := Register(d); err != nil {
			panic(err)
		}
	}
}
