package plugincontrol

import (
	"context"
	"maps"

	"github.com/fnMrRice/MusicFS-server/sdk/plugindisplay"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

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
	counts := make(map[string]func() *plugindisplay.Count)
	for id, runtime := range m.runtimes {
		if runtime.hooks.InstanceName != nil {
			names[id] = runtime.hooks.InstanceName
		}
		if runtime.hooks.AccountName != nil {
			accounts[id] = runtime.hooks.AccountName
		}
		if pluginconfig.SupportsDirectories(runtime.plugin.PluginID) {
			// Deployment permissions remain meaningful before a worker starts.
			// Never send unselected directory paths to the provider process.
			count := localAllowedDirectoryCount(runtime.instance)
			counts[id] = func() *plugindisplay.Count { return count.Clone() }
		} else if runtime.hooks.DisplayCount != nil {
			counts[id] = runtime.hooks.DisplayCount
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
		if scoped {
			snapshot.Items[pi].DisplayCount = collectDisplayCount(instances, counts)
		}
	}
	return snapshot
}

func localAllowedDirectoryCount(instance config.Instance) *plugindisplay.Count {
	return &plugindisplay.Count{Value: int64(len(config.AllowedDirectories(instance))), Unit: "allowed directories", LocalizedUnits: map[string]string{
		"zh": "个允许访问的目录", "zh-Hans": "个允许访问的目录", "zh-Hant": "個允許存取的目錄", "ja": "個のアクセス許可ディレクトリ",
	}}
}

// Counts are collected only after scope filtering and outside the manager lock.
// A partial total is misleading, so every visible runtime must supply valid,
// consistently labelled plugin data. An empty scope has no plugin-owned count.
func collectDisplayCount(instances []Instance, hooks map[string]func() *plugindisplay.Count) *plugindisplay.Count {
	var total *plugindisplay.Count
	for _, instance := range instances {
		resolve := hooks[instance.InstanceID]
		if resolve == nil {
			return nil
		}
		count := resolve()
		if count == nil || count.Validate() != nil {
			return nil
		}
		if total == nil {
			total = count.Clone()
			continue
		}
		if total.Unit != count.Unit || !maps.Equal(total.LocalizedUnits, count.LocalizedUnits) || count.Value > plugindisplay.MaxValue-total.Value {
			return nil
		}
		total.Value += count.Value
	}
	return total
}
