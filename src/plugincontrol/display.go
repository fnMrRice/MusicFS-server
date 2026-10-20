package plugincontrol

import "context"

// SnapshotFor resolves request-scoped account names without retaining them in
// global runtime state or holding the manager lock during provider calls.
func (m *Manager) SnapshotFor(ctx context.Context, owner string) Snapshot {
	snapshot := m.Snapshot()
	m.mu.Lock()
	names := make(map[string]func(context.Context, string) string)
	for id, runtime := range m.runtimes {
		if runtime.hooks.InstanceName != nil {
			names[id] = runtime.hooks.InstanceName
		}
	}
	m.mu.Unlock()
	for pi := range snapshot.Items {
		for ii := range snapshot.Items[pi].Instances {
			instance := &snapshot.Items[pi].Instances[ii]
			if resolve := names[instance.InstanceID]; resolve != nil && ctx.Err() == nil {
				if name := resolve(ctx, owner); name != "" {
					instance.Name = name
				}
			}
		}
	}
	return snapshot
}
