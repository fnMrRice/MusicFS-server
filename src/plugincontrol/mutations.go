package plugincontrol

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

func (m *Manager) Change(ctx context.Context, owner string, change config.Change) (Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if change.PluginID == m.storagePluginID() || change.PluginID == "musicfs.plugin.storage.sqlite" || change.PluginID == "musicfs.dsp.gain" {
		return Operation{}, domain.Fail("UNSUPPORTED", "Infrastructure plugins are controlled by startup configuration")
	}
	if m.store == nil || m.repo == nil {
		return Operation{}, domain.Fail("CONFIG_READ_ONLY", "Configuration file is not available for management")
	}
	if change.Mode == "" {
		change.Mode = "drain"
	}
	if change.Mode != "drain" && change.Mode != "force" || change.Enabled && change.Mode == "force" && change.Settings == nil {
		return Operation{}, domain.Fail("INVALID_ARGUMENT", "Invalid plugin transition mode")
	}
	if len(change.IdempotencyKey) < 8 || len(change.IdempotencyKey) > 128 {
		return Operation{}, domain.Fail("INVALID_ARGUMENT", "Idempotency key must contain 8 to 128 characters")
	}
	kind := "state"
	if change.Settings != nil {
		kind = "configuration"
	}
	change.IdempotencyKey = fmt.Sprintf("%x", sha256.Sum256([]byte(owner+"\x00"+change.PluginID+"\x00"+change.InstanceID+"\x00"+kind+"\x00"+change.IdempotencyKey)))
	found := false
	for _, p := range m.desired.Plugins {
		if p.PluginID == change.PluginID {
			found = change.InstanceID == ""
			for _, i := range p.Instances {
				found = found || i.InstanceID == change.InstanceID
			}
			break
		}
	}
	if !found {
		return Operation{}, domain.Fail("NOT_FOUND", "Configured plugin or instance not found")
	}
	saved, err := m.store.SetEnabled(ctx, change)
	if err != nil {
		return Operation{}, err
	}
	if old, e := m.repo.GetPluginOperation(ctx, owner, saved.ID); e == nil {
		return old, nil
	} else {
		var detail *domain.Error
		if !errors.As(e, &detail) || detail.Code != "NOT_FOUND" {
			return Operation{}, e
		}
	}
	now := time.Now().UTC()
	op := Operation{ID: saved.ID, Owner: owner, Revision: saved.NextRevision, PluginID: change.PluginID, InstanceID: change.InstanceID, Mode: change.Mode, Status: "applying", CreatedAt: now, UpdatedAt: now, Targets: map[string]int64{}}
	current := m.store.Snapshot()
	if saved.NextRevision != current.Revision {
		op.Status = "superseded"
		if err = m.repo.SavePluginOperation(ctx, op); err != nil {
			m.pending[op.ID] = &pendingOperation{Operation: op, dirty: true}
		}
		return op, err
	}
	m.desired = m.store.Current()
	m.revision = current.Revision
	if change.Settings == nil && !change.Enabled {
		// Both legacy modes now only save intent. Do not drain or cancel work,
		// and do not supersede a configuration application already in progress.
		op.Status = "succeeded"
	} else {
		m.applyChangeLocked(change, &op)
	}
	op.RestartRequired = m.restartRequiredLocked()
	pending := &pendingOperation{Operation: op, dirty: true}
	m.pending[op.ID] = pending
	m.publishLocked()
	if err = m.repo.SavePluginOperation(ctx, op); err != nil {
		return op, domain.Fail("TEMPORARY_FAILURE", "Plugin configuration saved; operation confirmation requires retry")
	}
	pending.dirty = false
	return op, nil
}

func (m *Manager) applyChangeLocked(change config.Change, op *Operation) {
	for _, p := range m.desired.Plugins {
		if p.PluginID != change.PluginID {
			continue
		}
		for _, i := range p.Instances {
			r := m.runtimes[i.InstanceID]
			if r == nil || change.InstanceID != "" && i.InstanceID != change.InstanceID {
				continue // Newly installed roles require startup registration.
			}
			// Settings and enable requests must never apply a pending disable
			// on either level, including a disable on the other level.
			activePlugin, activeInstance := p, i
			activePlugin.Enabled, activeInstance.Enabled = r.plugin.Enabled, r.instance.Enabled
			if change.Settings == nil {
				if change.InstanceID == "" {
					activePlugin.Enabled = true
				} else {
					activeInstance.Enabled = true
				}
			}
			r.plugin, r.instance = activePlugin, activeInstance
			if change.Settings == nil && r.state == "ready" {
				op.Targets[i.InstanceID] = r.generation
				continue
			}
			r.generation++
			r.reason = nil
			r.attempted = false
			r.stopFailed = false
			r.reconfigure = r.reconfigure || change.Settings != nil
			r.gate.SetAccepting(false, false)
			if r.running {
				r.state = "draining"
			} else if r.plugin.CanStart(r.instance) {
				r.state = "starting"
			} else {
				r.state = "disabled"
			}
			op.Targets[i.InstanceID] = r.generation
		}
	}
}

func (m *Manager) InstanceConfig(id string) (config.Plugin, config.Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.runtimes[id]
	if r == nil {
		return config.Plugin{}, config.Instance{}, domain.Fail("NOT_FOUND", "Plugin instance not found")
	}
	// The returned configuration is marshaled only through the safe projection.
	return r.plugin, r.instance, nil
}
