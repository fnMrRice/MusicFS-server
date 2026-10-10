package plugincontrol

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

func (m *Manager) Change(ctx context.Context, owner string, change config.Change) (Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	targets := []string{}
	for id, r := range m.runtimes {
		if r.plugin.PluginID == change.PluginID && (change.InstanceID == "" || change.InstanceID == id) {
			targets = append(targets, id)
		}
	}
	foundPlugin := false
	for _, p := range m.desired.Plugins {
		if p.PluginID == change.PluginID {
			foundPlugin = true
			break
		}
	}
	if len(targets) == 0 && (!foundPlugin || change.InstanceID != "") {
		return Operation{}, domain.Fail("NOT_FOUND", "Configured plugin or instance not found")
	}
	sort.Strings(targets)
	// Serialize durable desired-state publication with admission. A request
	// either acquires before the save or observes the new closed gate.
	for _, id := range targets {
		m.runtimes[id].gate.mu.Lock()
	}
	defer func() {
		for _, id := range targets {
			m.runtimes[id].gate.mu.Unlock()
		}
	}()
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
	for _, p := range m.desired.Plugins {
		for _, i := range p.Instances {
			r := m.runtimes[i.InstanceID]
			if r == nil {
				continue // Newly installed roles require startup registration.
			}
			if p.PluginID != change.PluginID || change.InstanceID != "" && i.InstanceID != change.InstanceID {
				continue
			}
			r.plugin = p
			r.instance = i
			r.generation++
			r.reason = nil
			r.attempted = false
			r.stopFailed = false
			// Once force has canceled a generation, a later enable must finish
			// reclaiming it. Only an ordinary healthy drain can be reversed.
			r.force = r.force || change.Mode == "force"
			r.reconfigure = r.reconfigure || change.Settings != nil
			r.gate.accepting = false
			r.gate.unavailableCode = "PLUGIN_DISABLED"
			if r.force {
				for l := range r.gate.leases {
					l.cancel()
				}
			}
			if r.running {
				r.state = "draining"
			} else if p.CanStart(i) {
				r.state = "starting"
			} else {
				r.state = "disabled"
			}
			op.Targets[i.InstanceID] = r.generation
		}
	}
	pending := &pendingOperation{Operation: op, dirty: true}
	m.pending[op.ID] = pending
	m.publishLocked()
	if err = m.repo.SavePluginOperation(ctx, op); err != nil {
		return op, domain.Fail("TEMPORARY_FAILURE", "Plugin configuration saved; operation confirmation requires retry")
	}
	pending.dirty = false
	return op, nil
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
