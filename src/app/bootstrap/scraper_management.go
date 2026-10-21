package bootstrap

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/scraper/local"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichmentworker"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

func registerScraperManagement(manager *plugincontrol.Manager, c config.Config, service *enrichmentworker.Service) error {
	for _, p := range c.Plugins {
		if p.PluginID == scraperlocal.PluginID {
			for _, i := range p.Instances {
				id := i.InstanceID
				r, _ := service.Runtime(id)
				r.Gate = manager.Gate(id)
				if err := service.UpdateRuntime(r); err != nil {
					return err
				}
				hooks := plugincontrol.Hooks{
					Ready:        func() bool { r, _ := service.Runtime(id); return r.Host != nil },
					Capabilities: func() []string { return []string{"lyrics", "artwork"} },
					PluginVersion: func() string {
						r, _ := service.Runtime(id)
						if r.Host == nil {
							return ""
						}
						return r.Host.PluginVersion()
					},
					Start: func(ctx context.Context, p config.Plugin, i config.Instance) error {
						if err := ctx.Err(); err != nil {
							return err
						}
						host, err := scraperlocal.NewHost(scraperlocal.Config{Builtin: p.InProcess(), WorkerPath: p.Executable, WorkerArgs: p.Args, WorkerSHA256: pluginChecksum(p), WorkDir: c.DataRoot, InstanceID: id, MaxConcurrent: 1})
						if err != nil {
							return err
						}
						return service.UpdateRuntime(enrichmentworker.Runtime{ID: id, Name: i.Name, Owner: "admin", Enabled: p.CanStart(i), Host: host, Gate: manager.Gate(id)})
					},
					Stop: func(ctx context.Context) error {
						gate := manager.Gate(id)
						for gate.Active() > 0 {
							timer := time.NewTimer(10 * time.Millisecond)
							select {
							case <-ctx.Done():
								timer.Stop()
								return ctx.Err()
							case <-timer.C:
							}
						}
						r, _ := service.Runtime(id)
						r.Host = nil
						r.Enabled = false
						return service.UpdateRuntime(r)
					},
				}
				if err := manager.Register(id, hooks, r.Host != nil); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
