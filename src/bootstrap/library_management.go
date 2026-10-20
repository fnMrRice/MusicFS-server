package bootstrap

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/scanwatch"
)

// configureLibraryManagement replaces configureWatch: every configured local
// instance owns its watcher and worker across configuration applications.
func configureLibraryManagement(runCtx context.Context, c config.Config, lib *library.Service, manager *plugincontrol.Manager, log *slog.Logger) (func(), error) {
	cleanups := []func(){}
	cleanup := func() {
		for _, close := range cleanups {
			close()
		}
	}
	for _, plugin := range c.Plugins {
		if plugin.PluginID != "musicfs.plugin.source.localfiles" {
			continue
		}
		for _, instance := range plugin.Instances {
			id := instance.InstanceID
			initial, ok := lib.Runtime(id)
			if !ok {
				cleanup()
				return func() {}, domain.Fail("NOT_FOUND", "Configured local provider not found")
			}
			gate := manager.Gate(id)
			if err := lib.SetGate(id, gate); err != nil {
				cleanup()
				return func() {}, err
			}
			var mu sync.Mutex
			var watcher *scanwatch.Watcher
			stopWatch := func() {
				mu.Lock()
				previous := watcher
				watcher = nil
				mu.Unlock()
				if previous != nil {
					previous.Close()
				}
			}
			startWatch := func(settings config.Instance, r library.Runtime) {
				current, err := startManagedLibraryWatch(runCtx, lib, settings, r, log)
				if err != nil {
					log.Warn("scan watch unavailable", "instanceId", id, "errorCode", pluginproto.ErrorCode(err))
					return
				}
				mu.Lock()
				watcher = current
				mu.Unlock()
			}
			ready := func() bool {
				r, ok := lib.Runtime(id)
				if !ok || r.Worker == nil {
					return false
				}
				select {
				case <-r.Worker.Done():
					return false
				default:
					return true
				}
			}
			stop := func(ctx context.Context) error {
				stopWatch()
				// Force cancellation originates at the gate. Wait for read handles
				// and exclusive writeback safe points before terminating the worker.
				if err := lib.WaitIdle(ctx, initial.Owner, id); err != nil {
					return err
				}
				r, _ := lib.Runtime(id)
				if r.Worker != nil {
					if err := r.Worker.Close(); err != nil {
						return err
					}
				}
				r.Worker = nil
				return lib.UpdateRuntime(r)
			}
			hooks := plugincontrol.Hooks{Ready: ready, Stop: stop, PluginIdentity: func() *pluginproto.PluginIdentity {
				r, ok := lib.Runtime(id)
				if !ok || r.Worker == nil {
					return nil
				}
				return r.Worker.PluginIdentity()
			}, Capabilities: func() []string {
				r, ok := lib.Runtime(id)
				if !ok || r.Worker == nil {
					return []string{}
				}
				return append([]string{}, r.Worker.Manifest.Capabilities...)
			}, Start: func(ctx context.Context, p config.Plugin, i config.Instance) error {
				if err := ctx.Err(); err != nil {
					return err
				}
				p.Instances = []config.Instance{i}
				desired := c
				desired.Plugins = []config.Plugin{p}
				runtimes, _, _, _ := configurePlugins(runCtx, desired, log)
				if len(runtimes) != 1 || runtimes[0].Worker == nil {
					return domain.Fail("SOURCE_OFFLINE", "Local provider could not be started")
				}
				r := runtimes[0]
				r.Gate = gate
				r.WriteBack = retainedWritebackRoots(initial, instance, i, r)
				if err := ctx.Err(); err != nil {
					r.Worker.Close()
					return err
				}
				if err := lib.UpdateRuntime(r); err != nil {
					r.Worker.Close()
					return err
				}
				startWatch(i, r)
				return nil
			}}
			if err := manager.Register(id, hooks, ready()); err != nil {
				cleanup()
				return func() {}, err
			}
			if ready() {
				r, _ := lib.Runtime(id)
				startWatch(instance, r)
			}
			cleanups = append(cleanups, func() {
				gate.SetAccepting(false, true)
				stopWatch()
				r, _ := lib.Runtime(id)
				if r.Worker != nil {
					_ = r.Worker.Close()
				}
			})
		}
	}
	return cleanup, nil
}

func retainedWritebackRoots(initial library.Runtime, before, after config.Instance, current library.Runtime) map[string]library.RootWriteBack {
	result := map[string]library.RootWriteBack{}
	for _, root := range after.Config.MusicRoots {
		result[root.ID] = library.RootWriteBack{Formats: []string{}, Fields: []string{}}
		for _, old := range before.Config.MusicRoots {
			if root.ID == old.ID && root.Path == old.Path {
				cap := initial.WriteBack[root.ID]
				if current.Worker != nil {
					for _, actual := range current.Worker.Access.Roots() {
						if actual.ID == root.ID && actual.Path == cap.BoundPath && actual.ExpectedIdentity == cap.BoundIdentity {
							result[root.ID] = cap
						}
					}
				}
			}
		}
	}
	return result
}

func startManagedLibraryWatch(ctx context.Context, lib *library.Service, instance config.Instance, r library.Runtime, log *slog.Logger) (*scanwatch.Watcher, error) {
	settings := instance.Config.Watch
	if settings == nil || !settings.Enabled || r.Worker == nil || r.Configuration == nil {
		return nil, nil
	}
	plan := pluginproto.ScanPlan{PlanVersion: pluginproto.ScanPlanVersion, Mode: "full", PluginID: r.Worker.Manifest.PluginID, PluginVersion: r.Worker.Manifest.PluginVersion, SchemaRevision: r.Configuration.SchemaRevision, ConfigRevision: r.ConfigRevision, Options: r.ImportDefaults}
	for _, root := range r.Worker.Access.Roots() {
		plan.Roots = append(plan.Roots, pluginproto.ScanRoot{RootID: root.ID, RootIdentity: root.ExpectedIdentity})
	}
	return scanwatch.Start(ctx, lib, scanwatch.Options{Owner: r.Owner, InstanceID: r.ID, Plan: plan, Access: r.Worker.Access, Debounce: time.Duration(settings.DebounceMs) * time.Millisecond, MaxDelay: time.Duration(settings.MaxDelayMs) * time.Millisecond, FullInterval: time.Duration(settings.FullRescanSeconds) * time.Second, Logger: log})
}
