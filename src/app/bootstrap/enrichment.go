package bootstrap

import (
	"context"
	"log/slog"

	_ "github.com/fnMrRice/MusicFS-server/plugins/scraper/local"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichmentworker"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func configureEnrichment(ctx context.Context, c config.Config, log *slog.Logger, store storage.Backend, lib *library.Service, content *mediastore.ContentService) (*enrichmentworker.Service, error) {
	runtimes := []enrichmentworker.Runtime{}
	available := false
	for _, plugin := range c.Plugins {
		registration, registered := enrichment.Lookup(plugin.PluginID)
		if !registered {
			continue
		}
		for _, instance := range plugin.Instances {
			runtime := enrichmentworker.Runtime{ID: instance.InstanceID, Name: instance.Name, Owner: "admin", Descriptor: registration.Descriptor, Enabled: plugin.CanStart(instance)}
			if plugin.CanStart(instance) {
				var err error
				runtime.Host, err = registration.Open(enrichment.HostConfig{Builtin: plugin.InProcess(), WorkerPath: plugin.Executable, WorkerArgs: plugin.Args, WorkerSHA256: pluginChecksum(plugin), WorkDir: c.DataRoot, InstanceID: instance.InstanceID, MaxConcurrent: 1})
				if err != nil {
					log.Warn("scraper instance unavailable", "pluginId", plugin.PluginID, "instanceId", instance.InstanceID, "errorCode", pluginproto.ErrorCode(err))
				}
			}
			available = available || runtime.Host != nil
			runtimes = append(runtimes, runtime)
		}
	}
	var decoder audioruntime.Decoder
	if available {
		// 后台解码使用独立的单任务实例，不占用播放或指纹任务的并发配额。
		decoder = backgroundDecoder(c, log)
	}
	service, err := enrichmentworker.New(store, lib, content, runtimes, enrichmentworker.Config{MaxConcurrent: 1, Decoder: decoder, Automatic: c.Metadata.Automatic})
	if err != nil {
		return nil, err
	}
	return service, nil
}
