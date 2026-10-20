package plugincontrol

import "context"

// SnapshotFor resolves request-scoped account names without retaining them in
// global runtime state or holding the manager lock during provider calls.
func (m *Manager) SnapshotFor(ctx context.Context, owner string) Snapshot {
	return m.snapshotFor(ctx, owner, false, false)
}

// SnapshotForScope includes shared instances and the user's online instances.
// The HTTP boundary must authorize administrator access before setting allUsers.
func (m *Manager) SnapshotForScope(ctx context.Context, owner string, allUsers bool) Snapshot {
	return m.snapshotFor(ctx, owner, true, allUsers)
}

func (m *Manager) snapshotFor(ctx context.Context, owner string, scoped, allUsers bool) Snapshot {
	snapshot := m.Snapshot()
	m.mu.Lock()
	names := make(map[string]func(context.Context, string) string)
	accounts := make(map[string]func(context.Context, string) string)
	for id, runtime := range m.runtimes {
		if runtime.hooks.InstanceName != nil {
			names[id] = runtime.hooks.InstanceName
		}
		if runtime.hooks.AccountName != nil {
			accounts[id] = runtime.hooks.AccountName
		}
	}
	m.mu.Unlock()
	for pi := range snapshot.Items {
		instances := make([]Instance, 0, len(snapshot.Items[pi].Instances))
		for _, instance := range snapshot.Items[pi].Instances {
			if scoped && !allUsers && instance.OwnerUserID != "" && instance.OwnerUserID != owner {
				continue
			}
			displayOwner := owner
			if allUsers && instance.OwnerUserID != "" {
				displayOwner = instance.OwnerUserID
			}
			if resolve := names[instance.InstanceID]; resolve != nil && ctx.Err() == nil {
				if name := resolve(ctx, displayOwner); name != "" {
					instance.Name = name
				}
			}
			if resolve := accounts[instance.InstanceID]; resolve != nil && ctx.Err() == nil {
				instance.AccountName = resolve(ctx, displayOwner)
			}
			instances = append(instances, instance)
		}
		snapshot.Items[pi].Instances = instances
	}
	return snapshot
}
