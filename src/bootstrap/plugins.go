package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/plugins/localfiles"
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
			enabled := plugin.CanStart(instance)
			var err error
			switch plugin.PluginID {
			case "musicfs.plugin.source.localfiles":
				name := instance.Name
				if name == "" {
					name = localfiles.Identity().Name
				}
				defaults, defaultsErr := config.EffectiveImportDefaults(plugin.PluginID, instance)
				declaration := pluginconfig.LocalFiles()
				binaryHash := ""
				if enabled {
					binaryHash = expectedChecksum(plugin.SHA256, plugin.Executable)
				}
				rootNames := map[string]string{}
				for _, root := range instance.Config.MusicRoots {
					rootNames[root.ID] = root.Name
				}
				// The bundled plugin owns its declaration even before roots are selected.
				// A successfully verified worker declaration supersedes this fallback.
				r := library.Runtime{ID: instance.InstanceID, Name: name, Owner: "admin", StableHash: true, Plugin: localfiles.Identity(),
					ImportDefaults: defaults, Configuration: declaration, RootNames: rootNames,
					ConfigRevision: providerConfigRevision(instance, binaryHash, defaults)}
				err = defaultsErr
				if enabled && err == nil {
					roots := make([]sourceaccess.RootConfig, 0, len(instance.Config.MusicRoots))
					for _, root := range instance.Config.MusicRoots {
						boundary, boundaryErr := config.RootBoundary(instance, root.Path)
						if boundaryErr != nil {
							err = boundaryErr
							break
						}
						roots = append(roots, sourceaccess.RootConfig{ID: root.ID, Path: root.Path, Boundary: boundary})
					}
					if err == nil {
						var stateDir string
						stateDir, err = instanceid.StateDirectory(filepath.Join(c.DataRoot, "providers"), instance.InstanceID)
						if err == nil {
							r.Worker, err = pluginhost.Start(ctx, pluginhost.Config{Executable: plugin.Executable, Args: plugin.Args, SHA256: binaryHash, InstanceID: instance.InstanceID, UserID: "admin", Roots: roots, StateDir: stateDir, DatabasePath: config.PluginDatabasePath(c)})
						}
					}
					if err == nil {
						// The verified worker must describe the same official options used by YAML.
						workerConfig := r.Worker.Manifest.Configuration
						if workerConfig == nil || workerConfig.SchemaRevision != declaration.SchemaRevision || workerConfig.ValidateOptions(defaults) != nil {
							r.Worker.Close()
							r.Worker = nil
							err = fmt.Errorf("PROTOCOL_MISMATCH: provider configuration declaration mismatch")
						} else {
							r.Configuration = workerConfig
						}
					}
				}
				runtimes = append(runtimes, r)
			case "musicfs.plugin.decoder.ffmpeg":
				if enabled {
					processor, err = audioffmpeg.NewProcessor(ctx, audioffmpeg.Config{InstanceID: instance.InstanceID, FFmpegPath: instance.Config.FFmpegPath, FFprobePath: instance.Config.FFprobePath, WorkerPath: plugin.Executable, WorkerArgs: plugin.Args, WorkerSHA256: expectedChecksum(plugin.SHA256, plugin.Executable), WorkDir: c.DataRoot, MaxConcurrent: c.MaxProcessing})
					if err == nil {
						ffprobe = instance.Config.FFprobePath
					}
				}
			case "musicfs.plugin.decoder.ncm":
				if enabled {
					decoder, err = ncmdecoder.NewProcessor(ncmdecoder.Config{InstanceID: instance.InstanceID, WorkerPath: plugin.Executable, WorkerArgs: plugin.Args, WorkerSHA256: expectedChecksum(plugin.SHA256, plugin.Executable), WorkDir: c.DataRoot, MaxConcurrent: c.MaxProcessing})
				}
			}
			if err != nil {
				log.Warn("plugin instance unavailable", "pluginId", plugin.PluginID, "instanceId", instance.InstanceID, "errorCode", pluginproto.ErrorCode(err))
			}
		}
	}
	return runtimes, processor, decoder, ffprobe
}
