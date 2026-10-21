package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/plugins/source/localfiles"
	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
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
					binaryHash = pluginChecksum(plugin)
				}
				rootNames := map[string]string{}
				rootDefaults := map[string]pluginproto.ImportOptions{}
				for _, root := range instance.Config.MusicRoots {
					rootNames[root.ID] = root.Name
					if root.ImportDefaults != nil {
						rootOptions, rootErr := config.EffectiveRootImportDefaults(instance, root)
						if rootErr != nil {
							defaultsErr = rootErr
						}
						rootDefaults[root.ID] = rootOptions
					}
				}
				// The bundled plugin owns its declaration even before roots are selected.
				// A successfully verified worker declaration supersedes this fallback.
				r := library.Runtime{ID: instance.InstanceID, Name: name, Owner: "admin", StableHash: true, Plugin: localfiles.Identity(),
					ImportDefaults: defaults, RootImportDefaults: rootDefaults, Configuration: declaration, RootNames: rootNames,
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
							r.Worker, err = pluginhost.Start(ctx, pluginhost.Config{Builtin: plugin.InProcess(), Executable: plugin.Executable, Args: plugin.Args, SHA256: binaryHash, InstanceID: instance.InstanceID, UserID: "admin", Roots: roots, StateDir: stateDir, DatabasePath: config.PluginDatabasePath(c), DatabaseBackend: config.PluginDatabaseBackend(c)})
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
					processor, err = audioffmpeg.NewProcessor(ctx, audioffmpeg.Config{Builtin: plugin.InProcess(), InstanceID: instance.InstanceID, FFmpegPath: instance.Config.FFmpegPath, FFprobePath: instance.Config.FFprobePath, WorkerPath: plugin.Executable, WorkerArgs: plugin.Args, WorkerSHA256: pluginChecksum(plugin), WorkDir: c.DataRoot, MaxConcurrent: c.MaxProcessing, DSPChain: c.AudioDSP.Chain, DSPPlugins: c.AudioDSP.Plugins, OutputLayout: c.AudioChannels.OutputLayout})
					if err == nil {
						ffprobe = instance.Config.FFprobePath
					}
				}
			case "musicfs.plugin.decoder.ncm":
				if enabled {
					decoder, err = ncmdecoder.NewProcessor(ncmdecoder.Config{Builtin: plugin.InProcess(), InstanceID: instance.InstanceID, WorkerPath: plugin.Executable, WorkerArgs: plugin.Args, WorkerSHA256: pluginChecksum(plugin), WorkDir: c.DataRoot, MaxConcurrent: c.MaxProcessing})
				}
			}
			if err != nil {
				log.Warn("plugin instance unavailable", "pluginId", plugin.PluginID, "instanceId", instance.InstanceID, "errorCode", pluginproto.ErrorCode(err))
			}
		}
	}
	return runtimes, processor, decoder, ffprobe
}
