package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/onlinehost"
	"github.com/fnMrRice/MusicFS-server/src/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func configureOnline(ctx context.Context, c config.Config, log *slog.Logger) (*onlinehost.Service, []onlinehost.Runtime, func(), error) {
	runtimes := []onlinehost.Runtime{}
	workers := []*pluginhost.OnlineWorker{}
	closeAll := func() {
		for _, worker := range workers {
			_ = worker.Close()
		}
	}
	for _, plugin := range c.Plugins {
		platform := config.OnlinePlatform(plugin.PluginID)
		if platform == "" {
			continue
		}
		for _, instance := range plugin.Instances {
			enabled := plugin.CanStart(instance)
			name := instance.Name
			if name == "" {
				name = pluginconfig.Identity(plugin.PluginID).Name
			}
			hash := ""
			if enabled {
				hash = expectedChecksum(plugin.SHA256, plugin.Executable)
			}
			binding, _ := json.Marshal(struct{ Plugin, Instance, Binary, Schema string }{plugin.PluginID, instance.InstanceID, hash, pluginproto.OnlineRevision})
			r := onlinehost.Runtime{Owner: "admin", InstanceID: instance.InstanceID, Name: name, Platform: platform, ConfigRevision: fmt.Sprintf("%x", sha256.Sum256(binding)), Enabled: enabled}
			if enabled {
				stateDir, err := instanceid.StateDirectory(filepath.Join(c.DataRoot, "online"), instance.InstanceID)
				var worker *pluginhost.OnlineWorker
				if err == nil {
					worker, err = pluginhost.StartOnline(ctx, pluginhost.OnlineConfig{Executable: plugin.Executable, Args: plugin.Args, SHA256: hash, StateDir: stateDir, Platform: platform, UserID: "admin", InstanceID: instance.InstanceID})
				}
				if err != nil {
					log.Warn("online plugin instance unavailable", "pluginId", plugin.PluginID, "instanceId", instance.InstanceID, "errorCode", pluginproto.ErrorCode(err))
				} else {
					r.Provider = worker
					workers = append(workers, worker)
				}
			}
			runtimes = append(runtimes, r)
		}
	}
	service, err := onlinehost.New(runtimes)
	if err != nil {
		closeAll()
		return nil, nil, nil, err
	}
	return service, runtimes, closeAll, nil
}
