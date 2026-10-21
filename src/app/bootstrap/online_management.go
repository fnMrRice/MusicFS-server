package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/plugindisplay"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library/recommendations"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
)

type managedOnline struct {
	mu              sync.Mutex
	run             context.Context
	dataRoot        string
	host            *onlinehost.Service
	recommendations *recommendations.Service
	runtime         onlinehost.Runtime
	worker          *pluginhost.OnlineWorker
}

func registerOnlineManagement(ctx context.Context, c config.Config, manager *plugincontrol.Manager, host *onlinehost.Service, runtimes []onlinehost.Runtime, recs *recommendations.Service) (func(), error) {
	states := []*managedOnline{}
	closeAll := func() {
		for _, s := range states {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			s.runtime.Gate.SetAccepting(false, true)
			_ = s.stop(ctx)
			cancel()
		}
	}
	for _, r := range runtimes {
		r.Gate = manager.Gate(r.InstanceID)
		if err := host.AttachGate(r.Owner, r.InstanceID, r.Gate); err != nil {
			return closeAll, err
		}
		worker, _ := r.Provider.(*pluginhost.OnlineWorker)
		s := &managedOnline{run: ctx, dataRoot: c.DataRoot, host: host, recommendations: recs, runtime: r, worker: worker}
		states = append(states, s)
		if err := s.updateRecommendations(); err != nil {
			return closeAll, err
		}
		if err := manager.Register(r.InstanceID, plugincontrol.Hooks{Start: s.start, Stop: s.stop, Ready: s.ready, PluginIdentity: func() *pluginproto.PluginIdentity {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.worker == nil {
				return nil
			}
			return s.worker.PluginIdentity()
		}, DisplayCount: func() *plugindisplay.Count {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.worker == nil {
				return nil
			}
			return s.worker.DisplayCount()
		}, PluginVersion: func() string {
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.worker == nil {
				return ""
			}
			return s.worker.PluginVersion()
		}, InstanceName: func(ctx context.Context, owner string) string {
			if owner != r.Owner {
				return ""
			}
			return host.InstanceName(ctx, owner, r.InstanceID)
		}, AccountName: func(ctx context.Context, owner string) string {
			if owner != r.Owner {
				return ""
			}
			return host.AccountName(ctx, owner, r.InstanceID)
		}, Capabilities: s.capabilities}, s.ready()); err != nil {
			return closeAll, err
		}
	}
	return closeAll, nil
}

func (s *managedOnline) capabilities() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := []string{}
	if s.worker != nil {
		for _, capability := range s.worker.Descriptor().Capabilities {
			if capability.Supported {
				result = append(result, capability.Name)
			}
		}
	}
	return result
}

func (s *managedOnline) ready() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.worker == nil {
		return false
	}
	select {
	case <-s.worker.Done():
		return false
	default:
		return true
	}
}

func (s *managedOnline) updateRecommendations() error {
	r := s.runtime
	item := recommendations.Runtime{Owner: r.Owner, InstanceID: r.InstanceID, Name: pluginconfig.Identity(pluginproto.OnlinePluginID(r.Platform)).Name, Platform: r.Platform, ConfigRevision: r.ConfigRevision, Gate: r.Gate,
		InstanceName: func(ctx context.Context) string { return s.host.InstanceName(ctx, r.Owner, r.InstanceID) }}
	// Keep the scoped availability check even without a running worker, so
	// explicit disable and startup failure retain distinct error semantics.
	item.Provider = s.host.RecommendationProvider(r.Owner, r.InstanceID)
	return s.recommendations.UpdateRuntime(item)
}

func (s *managedOnline) start(ctx context.Context, p config.Plugin, i config.Instance) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.worker != nil {
		if err := s.worker.Close(); err != nil {
			return err
		}
		s.worker = nil
	}
	name := i.Name
	if name == "" {
		name = pluginconfig.Identity(p.PluginID).Name
	}
	hash := pluginChecksum(p)
	owner := i.OnlineOwnerUserID()
	binding, _ := json.Marshal(struct{ Plugin, Instance, Owner, Binary, Schema string }{p.PluginID, i.InstanceID, owner, hash, pluginproto.OnlineRevision})
	r := onlinehost.Runtime{Owner: owner, InstanceID: i.InstanceID, Name: name, Platform: config.OnlinePlatform(p.PluginID), ConfigRevision: fmt.Sprintf("%x", sha256.Sum256(binding)), Enabled: true, Gate: s.runtime.Gate}
	stateDir, err := instanceid.StateDirectory(filepath.Join(s.dataRoot, "online"), i.InstanceID)
	if err != nil {
		return err
	}
	w, err := pluginhost.StartOnline(s.run, pluginhost.OnlineConfig{Builtin: p.InProcess(), Executable: p.Executable, Args: p.Args, SHA256: hash, StateDir: stateDir, Platform: r.Platform, UserID: r.Owner, InstanceID: r.InstanceID})
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		_ = w.Close()
		return err
	}
	r.Provider = w
	if err = s.host.UpdateRuntime(r); err != nil {
		_ = w.Close()
		return err
	}
	s.worker, s.runtime = w, r
	if err = s.updateRecommendations(); err != nil {
		_ = w.Close()
		s.worker = nil
		r.Provider = nil
		r.Enabled = false
		_ = s.host.UpdateRuntime(r)
		return err
	}
	return nil
}

func (s *managedOnline) stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	for s.runtime.Gate != nil && s.runtime.Gate.Active() > 0 {
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	r := s.runtime
	r.Enabled = false
	r.Provider = nil
	if err := s.host.UpdateRuntime(r); err != nil {
		return err
	}
	s.runtime = r
	var err error
	if s.worker != nil {
		err = s.worker.Close()
		if err == nil {
			s.worker = nil
		}
	}
	if updateErr := s.updateRecommendations(); err == nil {
		err = updateErr
	}
	return err
}
