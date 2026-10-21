package plugincontrol

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
)

type action struct {
	id         string
	generation int64
	start      bool
	hooks      Hooks
	plugin     config.Plugin
	instance   config.Instance
}

func (m *Manager) reconcile() {
	m.mu.Lock()
	actions := []action{}
	changed := false
	for id, r := range m.runtimes {
		if r.stopFailed {
			continue
		}
		want := r.plugin.CanStart(r.instance)
		if !r.running && r.plugin.Pending(r.instance) {
			if r.state != "pending_configuration" {
				r.state = "pending_configuration"
				code := "CONFIGURATION_REQUIRED"
				r.reason = &code
				changed = true
			}
			r.gate.SetAccepting(false, false)
			continue
		}
		if r.running && r.hooks.Ready != nil && !r.hooks.Ready() {
			r.gate.SetUnavailable("SOURCE_OFFLINE", true)
			r.state = "failed"
			code := "WORKER_EXITED"
			r.reason = &code
			r.attempted = true
			// Cleanup also runs for a worker that exited unexpectedly.
			r.force = true
			r.reconfigure = true
			changed = true
		}
		if r.running && want && !r.reconfigure && !r.force {
			if r.state != "ready" {
				r.state = "ready"
				r.gate.SetAccepting(true, false)
				changed = true
			}
			continue
		}
		if r.running {
			if r.state == "failed" && want {
				r.gate.SetUnavailable("SOURCE_OFFLINE", r.force)
			} else {
				r.gate.SetAccepting(false, r.force)
			}
			if r.gate.Active() > 0 && !r.force {
				if r.state != "draining" {
					r.state = "draining"
					changed = true
				}
				continue
			}
			actions = append(actions, action{id: id, generation: r.generation, hooks: r.hooks})
			continue
		}
		if want && !r.attempted {
			r.gate.Rotate()
			r.state = "starting"
			r.attempted = true
			changed = true
			actions = append(actions, action{id: id, generation: r.generation, start: true, hooks: r.hooks, plugin: r.plugin, instance: r.instance})
		} else if !want && r.state != "disabled" {
			r.state = "disabled"
			changed = true
		}
	}
	if changed {
		m.publishLocked()
	}
	m.mu.Unlock()
	for _, a := range actions {
		m.mu.Lock()
		current := m.runtimes[a.id].generation == a.generation
		m.mu.Unlock()
		if !current {
			continue
		}
		ctx, cancel := context.WithTimeout(m.ctx, 15*time.Second)
		var err error
		if a.start {
			if a.hooks.Start != nil {
				err = a.hooks.Start(ctx, a.plugin, a.instance)
			}
		} else if a.hooks.Stop != nil {
			err = a.hooks.Stop(ctx)
		}
		cancel()
		m.mu.Lock()
		r := m.runtimes[a.id]
		if a.start {
			r.running = err == nil && a.hooks.Ready != nil && a.hooks.Ready()
			if r.generation == a.generation {
				r.force = false
				r.reconfigure = false
				if r.running {
					r.state = "ready"
					r.gate.SetAccepting(true, false)
				} else {
					r.state = "failed"
					r.gate.SetUnavailable("SOURCE_OFFLINE", false)
					code := "START_FAILED"
					if err != nil {
						code = pluginproto.ErrorCode(err)
					}
					r.reason = &code
				}
			} else if r.running {
				r.reconfigure = true
			}
		} else if err != nil {
			r.state = "failed"
			code := "STOP_FAILED"
			r.reason = &code
			r.attempted = true
			// Keep it tracked and closed; failed stops require a new explicit request.
			r.force = false
			r.stopFailed = true
		} else {
			r.running = false
			r.force = false
			r.reconfigure = false
			if r.generation == a.generation && r.state == "failed" {
				r.attempted = true
			} else {
				r.attempted = false
				r.state = "disabled"
			}
		}
		m.publishLocked()
		m.mu.Unlock()
	}
	m.finishOperations()
}

func (m *Manager) finishOperations() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, op := range m.pending {
		status, reason := op.Status, op.ErrorCode
		if !terminalOperation(status) {
			status, reason = "succeeded", nil
		}
		for target, generation := range op.Targets {
			if terminalOperation(op.Status) {
				break
			}
			r := m.runtimes[target]
			if r.generation != generation {
				status = "superseded"
				break
			}
			if r.state == "failed" {
				status = "failed"
				reason = r.reason
				break
			}
			want := r.plugin.CanStart(r.instance)
			if want && r.state != "ready" || !want && r.state != "disabled" && r.state != "pending_configuration" {
				status = "draining"
			}
		}
		if status != op.Status || !sameReason(op.ErrorCode, reason) {
			op.Status = status
			op.ErrorCode = reason
			op.UpdatedAt = time.Now().UTC()
			op.dirty = true
		}
		if op.dirty {
			ctx, cancel := context.WithTimeout(m.ctx, time.Second)
			err := m.repo.SavePluginOperation(ctx, op.Operation)
			cancel()
			op.dirty = err != nil
		}
		if !op.dirty && terminalOperation(status) {
			delete(m.pending, id)
			m.publishLocked()
		}
	}
	allApplied := !m.restartRequiredLocked()
	for _, r := range m.runtimes {
		want := r.plugin.CanStart(r.instance)
		if want && r.state != "ready" || !want && r.state != "disabled" && r.state != "pending_configuration" {
			allApplied = false
			break
		}
	}
	if allApplied && m.applied != m.revision && m.store != nil {
		ctx, cancel := context.WithTimeout(m.ctx, time.Second)
		err := m.store.MarkApplied(ctx, m.revision)
		cancel()
		if err == nil {
			m.applied = m.revision
			m.publishLocked()
		}
	}
}

func terminalOperation(status string) bool {
	return status == "succeeded" || status == "superseded" || status == "failed"
}

func sameReason(a, b *string) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
