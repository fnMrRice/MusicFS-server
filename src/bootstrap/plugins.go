package bootstrap

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/plugins/ncmdecoder"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

// 父插件和实例必须同时显式启用；禁用项只保留提供者身份，不检查运行依赖。
func configurePlugins(ctx context.Context, c config.Config, log *slog.Logger) ([]library.Runtime, *audioffmpeg.Processor, *ncmdecoder.Processor, string) {
	var runtimes []library.Runtime
	var processor *audioffmpeg.Processor
	var decoder *ncmdecoder.Processor
	ffprobe := ""
	for _, plugin := range c.Plugins {
		for _, instance := range plugin.Instances {
			enabled := plugin.Enabled && instance.Enabled
			var err error
			switch plugin.PluginID {
			case "musicfs.localfiles":
				name := instance.Name
				if name == "" {
					name = instance.InstanceID
				}
				r := library.Runtime{ID: instance.InstanceID, Name: name, Owner: "admin"}
				if enabled {
					roots := make([]sourceaccess.RootConfig, 0, len(instance.Config.MusicRoots))
					for _, root := range instance.Config.MusicRoots {
						roots = append(roots, sourceaccess.RootConfig{ID: root.ID, Path: root.Path})
					}
					r.Worker, err = pluginhost.Start(ctx, pluginhost.Config{Executable: plugin.Executable, SHA256: expectedChecksum(plugin.SHA256, plugin.Executable), InstanceID: instance.InstanceID, UserID: "admin", Roots: roots, StateDir: filepath.Join(c.DataRoot, "providers", instance.InstanceID)})
				}
				runtimes = append(runtimes, r)
			case "musicfs.audio.ffmpeg":
				if enabled {
					processor, err = audioffmpeg.NewProcessor(ctx, audioffmpeg.Config{FFmpegPath: instance.Config.FFmpegPath, FFprobePath: instance.Config.FFprobePath, WorkerPath: plugin.Executable, WorkerSHA256: expectedChecksum(plugin.SHA256, plugin.Executable), WorkDir: c.DataRoot, MaxConcurrent: c.MaxProcessing})
					if err == nil {
						ffprobe = instance.Config.FFprobePath
					}
				}
			case "musicfs.ncm":
				if enabled {
					decoder, err = ncmdecoder.NewProcessor(ncmdecoder.Config{WorkerPath: plugin.Executable, WorkerSHA256: expectedChecksum(plugin.SHA256, plugin.Executable), WorkDir: c.DataRoot, MaxConcurrent: c.MaxProcessing})
				}
			}
			if err != nil {
				log.Warn("plugin instance unavailable", "pluginId", plugin.PluginID, "instanceId", instance.InstanceID, "errorCode", pluginproto.ErrorCode(err))
			}
		}
	}
	return runtimes, processor, decoder, ffprobe
}
