package bootstrap

import (
	"context"
	"log/slog"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/scanwatch"
)

func configureWatch(ctx context.Context, c config.Config, lib *library.Service, runtimes []library.Runtime, log *slog.Logger) func() {
	var watchers []*scanwatch.Watcher
	for _, plugin := range c.Plugins {
		if plugin.PluginID != "musicfs.plugin.source.localfiles" || !plugin.Enabled {
			continue
		}
		for _, instance := range plugin.Instances {
			settings := instance.Config.Watch
			if !instance.Enabled || settings == nil || !settings.Enabled {
				continue
			}
			for _, runtime := range runtimes {
				if runtime.ID != instance.InstanceID {
					continue
				}
				if runtime.Worker == nil || runtime.Configuration == nil {
					log.Warn("scan watch paused until restart", "instanceId", instance.InstanceID, "errorCode", "SOURCE_OFFLINE")
					continue
				}
				roots := make([]string, 0, len(instance.Config.MusicRoots))
				for _, root := range instance.Config.MusicRoots {
					roots = append(roots, root.ID)
				}
				plan, err := lib.BuildScanPlan(ctx, runtime.Owner, runtime.ID, library.ScanInput{Mode: "full", SchemaRevision: runtime.Configuration.SchemaRevision, RootIDs: roots})
				if err == nil {
					var watcher *scanwatch.Watcher
					watcher, err = scanwatch.Start(ctx, lib, scanwatch.Options{Owner: runtime.Owner, InstanceID: runtime.ID, Plan: plan, Access: runtime.Worker.Access,
						Debounce: time.Duration(settings.DebounceMs) * time.Millisecond, MaxDelay: time.Duration(settings.MaxDelayMs) * time.Millisecond,
						FullInterval: time.Duration(settings.FullRescanSeconds) * time.Second, Logger: log})
					if err == nil {
						watchers = append(watchers, watcher)
					}
				}
				if err != nil {
					log.Warn("scan watch unavailable", "instanceId", runtime.ID, "errorCode", pluginproto.ErrorCode(err))
				}
			}
		}
	}
	return func() {
		for _, watcher := range watchers {
			watcher.Close()
		}
	}
}
