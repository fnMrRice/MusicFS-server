package bootstrap

import (
	"context"
	"log/slog"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/plugins/scraper/local"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/enrichmentworker"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

func configureEnrichment(ctx context.Context, c config.Config, log *slog.Logger, store storage.Backend, lib *library.Service, content *mediastore.ContentService) (*enrichmentworker.Service, error) {
	runtimes := []enrichmentworker.Runtime{}
	available := false
	for _, plugin := range c.Plugins {
		if plugin.PluginID != scraperlocal.PluginID {
			continue
		}
		for _, instance := range plugin.Instances {
			runtime := enrichmentworker.Runtime{ID: instance.InstanceID, Name: instance.Name, Owner: "admin", Enabled: plugin.CanStart(instance)}
			if plugin.CanStart(instance) {
				var err error
				runtime.Host, err = scraperlocal.NewHost(scraperlocal.Config{WorkerPath: plugin.Executable, WorkerArgs: plugin.Args, WorkerSHA256: expectedChecksum(plugin.SHA256, plugin.Executable), WorkDir: c.DataRoot, InstanceID: instance.InstanceID, MaxConcurrent: 1})
				if err != nil {
					log.Warn("scraper instance unavailable", "pluginId", plugin.PluginID, "instanceId", instance.InstanceID, "errorCode", pluginproto.ErrorCode(err))
				}
			}
			available = available || runtime.Host != nil
			runtimes = append(runtimes, runtime)
		}
	}
	var decoder *ncmdecoder.Processor
	if available {
		// A fresh processor provides a separate one-slot background lane; it is
		// not shared with playback or the fingerprint worker's NCM admission.
		decoder = fingerprintNCMDecoder(c, log)
	}
	service, err := enrichmentworker.New(store, lib, content, runtimes, enrichmentworker.Config{MaxConcurrent: 1, NCMDecoder: decoder, Automatic: c.Metadata.Automatic})
	if err != nil {
		return nil, err
	}
	return service, nil
}
