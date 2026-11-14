package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

// 已停用的来源也保留实例归属，已入库的插件内编号仍引用原插件。
func musicSourcePlugins(c config.Config) map[string]string {
	providers := make(map[string]string)
	for _, plugin := range c.Plugins {
		if pluginconfig.Role(plugin.PluginID) == "provider" {
			for _, instance := range plugin.Instances {
				providers[instance.InstanceID] = plugin.PluginID
			}
		}
	}
	return providers
}

// 父插件和实例必须同时显式启用；禁用项只保留提供者身份，不检查运行依赖。
func configurePlugins(ctx context.Context, c config.Config, log *slog.Logger) ([]library.Runtime, audioruntime.Processor, audioruntime.Decoder, audioruntime.Probe) {
	var runtimes []library.Runtime
	var processor audioruntime.Processor
	var decoder audioruntime.Decoder
	var probe audioruntime.Probe
	for _, plugin := range c.Plugins {
		for _, instance := range plugin.Instances {
			enabled := plugin.CanStart(instance)
			var err error
			switch {
			case pluginconfig.SupportsDirectories(plugin.PluginID):
				name := instance.Name
				if name == "" {
					name = pluginconfig.Identity(plugin.PluginID).Name
				}
				defaults, defaultsErr := config.EffectiveImportDefaults(plugin.PluginID, instance)
				declaration := pluginconfig.Configuration(plugin.PluginID)
				binaryHash := ""
				if enabled {
					binaryHash = pluginChecksum(plugin)
				}
				rootNames := map[string]string{}
				rootDefaults := map[string]pluginproto.ImportOptions{}
				for _, root := range instance.Config.MusicRoots {
					rootNames[root.ID] = root.Name
					if root.ImportDefaults != nil {
						rootOptions, rootErr := config.EffectiveRootImportDefaults(plugin.PluginID, instance, root)
						if rootErr != nil {
							defaultsErr = rootErr
						}
						rootDefaults[root.ID] = rootOptions
					}
				}
				// The bundled plugin owns its declaration even before roots are selected.
				// A successfully verified worker declaration supersedes this fallback.
				registered, _ := pluginconfig.Lookup(plugin.PluginID)
				r := library.Runtime{ID: instance.InstanceID, Name: name, Owner: "admin", SourceType: registered.SourceType, StableHash: true, Plugin: pluginconfig.Identity(plugin.PluginID),
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
							r.Worker, err = pluginhost.Start(ctx, pluginhost.Config{PluginID: plugin.PluginID, Builtin: plugin.InProcess(), Executable: plugin.Executable, Args: plugin.Args, SHA256: binaryHash, InstanceID: instance.InstanceID, UserID: "admin", Roots: roots, StateDir: stateDir, DatabasePath: config.PluginDatabasePath(c), DatabaseBackend: config.PluginDatabaseBackend(c)})
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
			default:
				if enabled {
					if factory, ok := audioFactory(plugin.PluginID); ok {
						var value AudioInstance
						value, err = createAudioInstance(ctx, factory, c, plugin, instance)
						if err == nil {
							if factory.Processing {
								processor, probe = value.Processor, value.Probe
							} else {
								decoder = value.Decoder
							}
						}
					}
				}
			}
			if err != nil {
				log.Warn("plugin instance unavailable", "pluginId", plugin.PluginID, "instanceId", instance.InstanceID, "errorCode", pluginproto.ErrorCode(err))
			}
		}
	}
	return runtimes, processor, decoder, probe
}
